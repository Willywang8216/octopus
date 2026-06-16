#!/usr/bin/env bash
set -euo pipefail

# Optional: load local env (not committed). Useful to avoid re-typing BASE_URL / ADMIN creds.
# Example .octopus.env:
#   export BASE_URL="http://172.18.0.39:8080"
#   export ADMIN_USER="..."
#   export ADMIN_PASS="..."
ENV_FILE="${ENV_FILE:-.octopus.env}"
if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$ENV_FILE"
fi

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-admin}"

# APPLY=1 will actually update groups (and delete groups if pruning enabled). Otherwise it's a dry run.
APPLY="${APPLY:-0}"

# If enabled, remove group items that point to disabled channels.
DROP_DISABLED_CHANNEL_ITEMS="${DROP_DISABLED_CHANNEL_ITEMS:-0}"

# If enabled, set Embeddings-DB-* groups mode to Failover.
SET_DB_EMBEDDING_GROUP_FAILOVER="${SET_DB_EMBEDDING_GROUP_FAILOVER:-1}"

# If enabled, apply a conservative embedding MatchRegex to groups with name containing "embed".
APPLY_EMBEDDING_REGEX="${APPLY_EMBEDDING_REGEX:-0}"
EMBEDDING_MATCH_REGEX_DEFAULT="${EMBEDDING_MATCH_REGEX_DEFAULT:-(?i)^(text-embedding-.*|.*embedding.*|.*embed.*|(?:baai/)?bge-.*|netease-youdao/bce-embedding-base_v1|(?:qwen/)?qwen3-embedding-.*)$}"

# If enabled, delete groups whose name matches PRUNE_GROUP_NAME_REGEX (bash regex).
# Common usage:
#   APPLY=0 PRUNE_AUDIT_GROUPS=1 scripts/ops/group_cleanup.sh   # dry run
#   APPLY=1 PRUNE_AUDIT_GROUPS=1 scripts/ops/group_cleanup.sh   # delete __audit__ch_* groups
PRUNE_AUDIT_GROUPS="${PRUNE_AUDIT_GROUPS:-0}"
PRUNE_GROUP_NAME_REGEX="${PRUNE_GROUP_NAME_REGEX:-}"
if [[ "$PRUNE_AUDIT_GROUPS" == "1" && -z "$PRUNE_GROUP_NAME_REGEX" ]]; then
  PRUNE_GROUP_NAME_REGEX="^__audit__ch_[0-9]+$"
fi

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing dependency: $1" >&2; exit 1; }
}
require curl
require jq

log(){ printf "[%s] %s\n" "$(date -u +'%F %T UTC')" "$*" >&2; }

validate_numeric() {
  local v="$1"
  [[ "$v" =~ ^[0-9]+$ ]]
}

is_rerank_model() {
  local m="$1"
  m="${m,,}"
  [[ "$m" == *rerank* ]]
}

is_embedding_model() {
  local m="$1"
  m="${m,,}"

  # explicitly exclude rerankers
  [[ "$m" == *rerank* ]] && return 1

  [[ "$m" == text-embedding-* ]] && return 0
  [[ "$m" == *embedding* ]] && return 0

  # Many providers use "embed" (not "embedding") in model names.
  # Examples: nvidia/nv-embed-v1, snowflake/arctic-embed-l, *-embed-v2
  [[ "$m" == *embed* ]] && return 0

  [[ "$m" == bge-* ]] && return 0
  [[ "$m" == *"/bge-"* ]] && return 0

  return 1
}

make_delete_ids_json() {
  # Convert numeric ids to a JSON array. Prints "[]" if empty.
  if [[ "$#" -eq 0 ]]; then
    printf '[]'
    return 0
  fi
  jq -n --args '$ARGS.positional | map(tonumber)' "$@"
}

ADMIN_JWT="$(
  curl -sS "$BASE_URL/api/v1/user/login" \
    -H "Content-Type: application/json" \
    -d "$(jq -n --arg u "$ADMIN_USER" --arg p "$ADMIN_PASS" '{username:$u,password:$p}')" \
  | jq -r '.data.token // empty'
)"

if [[ -z "$ADMIN_JWT" ]]; then
  log "ERROR: admin login failed"
  exit 1
fi

apply_group_update() {
  local payload="$1"
  if [[ "$APPLY" == "1" ]]; then
    curl -sS "$BASE_URL/api/v1/group/update" \
      -H "Authorization: Bearer $ADMIN_JWT" \
      -H "Content-Type: application/json" \
      -d "$payload" \
    | jq -e '.code==200' >/dev/null
  else
    echo "$payload" | jq . >/dev/null
  fi
}

apply_group_delete() {
  local gid="$1"
  local gname="$2"
  if [[ "$APPLY" == "1" ]]; then
    curl -sS -X DELETE "$BASE_URL/api/v1/group/delete/$gid" \
      -H "Authorization: Bearer $ADMIN_JWT" \
    | jq -e '.code==200' >/dev/null
  else
    log "DRY  delete group id=$gid name=$gname"
  fi
}

log "Fetching channels..."
channels_json="$(curl -sS "$BASE_URL/api/v1/channel/list" -H "Authorization: Bearer $ADMIN_JWT")"

# Map channel_id -> {type, enabled, name}
declare -A CH_TYPE
declare -A CH_ENABLED
declare -A CH_NAME

# Delimiter is '|'. If you have a channel name containing '|', switch delimiter.
while IFS='|' read -r id typ enabled name; do
  CH_TYPE["$id"]="$typ"
  CH_ENABLED["$id"]="$enabled"
  CH_NAME["$id"]="$name"
done < <(echo "$channels_json" | jq -r '.data[] | "\(.id)|\(.type)|\(.enabled)|\(.name)"')

log "Fetching groups..."
groups_json="$(curl -sS "$BASE_URL/api/v1/group/list" -H "Authorization: Bearer $ADMIN_JWT")"

group_count="$(echo "$groups_json" | jq -r '.data | length')"
log "Groups: $group_count"

log "Scanning groups (APPLY=$APPLY)..."

echo "$groups_json" | jq -c '.data[]' | while read -r g; do
  gid="$(echo "$g" | jq -r '.id')"
  gname="$(echo "$g" | jq -r '.name')"
  gmode="$(echo "$g" | jq -r '.mode')"

  if ! validate_numeric "$gid"; then
    log "WARN group has invalid id: name=$gname id=$gid"
    continue
  fi

  if [[ -n "$PRUNE_GROUP_NAME_REGEX" ]] && [[ "$gname" =~ $PRUNE_GROUP_NAME_REGEX ]]; then
    log "PRUNE group=$gname id=$gid"
    apply_group_delete "$gid" "$gname"
    continue
  fi

  delete_ids=()

  while IFS='|' read -r item_id channel_id model_name; do
    if ! validate_numeric "$item_id"; then
      continue
    fi

    # Rerank models are not handled by this script (Octopus does not have a rerank endpoint).
    # Keep them untouched to avoid breaking user-maintained reranker groups.
    if is_rerank_model "$model_name"; then
      continue
    fi

    ctype="${CH_TYPE[$channel_id]:-}"
    cenabled="${CH_ENABLED[$channel_id]:-}"

    # If channel is unknown, drop the item.
    if [[ -z "$ctype" ]]; then
      delete_ids+=("$item_id")
      continue
    fi

    # Optional: drop items referencing disabled channels.
    if [[ "$DROP_DISABLED_CHANNEL_ITEMS" == "1" && "$cenabled" != "true" ]]; then
      delete_ids+=("$item_id")
      continue
    fi

    if is_embedding_model "$model_name"; then
      # embedding model should only live on embedding channels (type=5)
      if [[ "$ctype" != "5" ]]; then
        delete_ids+=("$item_id")
      fi
    else
      # chat model should not live on embedding channels (type=5)
      if [[ "$ctype" == "5" ]]; then
        delete_ids+=("$item_id")
      fi
    fi

  done < <(echo "$g" | jq -r '.items[]? | "\(.id)|\(.channel_id)|\(.model_name)"')

  # Optional: DB embedding groups should be Failover.
  next_mode=""
  if [[ "$SET_DB_EMBEDDING_GROUP_FAILOVER" == "1" && "$gname" == Embeddings-DB-* ]]; then
    if [[ "$gmode" != "3" ]]; then
      next_mode="3"
    fi
  fi

  next_match_regex=""
  if [[ "$APPLY_EMBEDDING_REGEX" == "1" ]]; then
    gname_lc="${gname,,}"
    if [[ "$gname_lc" == *embed* ]]; then
      cur_re="$(echo "$g" | jq -r '.match_regex // ""')"
      if [[ -z "$cur_re" ]]; then
        next_match_regex="$EMBEDDING_MATCH_REGEX_DEFAULT"
      fi
    fi
  fi

  if [[ "${#delete_ids[@]}" -eq 0 && -z "$next_mode" && -z "$next_match_regex" ]]; then
    log "OK   group=$gname"
    continue
  fi

  if [[ "${#delete_ids[@]}" -gt 0 ]]; then
    log "FIX  group=$gname delete_items=${#delete_ids[@]}"
    for id in "${delete_ids[@]}"; do
      item="$(echo "$g" | jq -c --argjson id "$id" '.items[]? | select(.id==$id)')"
      if [[ -z "$item" ]]; then
        continue
      fi
      ch_id="$(echo "$item" | jq -r '.channel_id')"
      ch_name="${CH_NAME[$ch_id]:-$ch_id}"
      log "  - item_id=$id channel=$ch_name model=$(echo "$item" | jq -r '.model_name')"
    done
  fi

  if [[ -n "$next_mode" ]]; then
    log "FIX  group=$gname set_mode=$next_mode (Failover)"
  fi

  if [[ -n "$next_match_regex" ]]; then
    log "FIX  group=$gname set_match_regex=$next_match_regex"
  fi

  del_json="$(make_delete_ids_json "${delete_ids[@]}")"
  mode_json="null"
  if [[ -n "$next_mode" ]]; then
    mode_json="$next_mode"
  fi

  payload="$(jq -n \
    --argjson id "$gid" \
    --argjson mode "$mode_json" \
    --arg match_regex "${next_match_regex:-}" \
    --argjson del "${del_json:-[]}" \
    '{id:$id}
      + (if $mode==null then {} else {mode:$mode} end)
      + (if $match_regex=="" then {} else {match_regex:$match_regex} end)
      + (if ($del|length)==0 then {} else {items_to_delete:$del} end)'
  )"

  apply_group_update "$payload"
done

log "Done. APPLY=$APPLY"
