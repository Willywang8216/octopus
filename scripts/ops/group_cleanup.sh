#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-admin}"

# APPLY=1 will actually update groups. Otherwise it's a dry run.
APPLY="${APPLY:-0}"

# If enabled, remove group items that point to disabled channels.
DROP_DISABLED_CHANNEL_ITEMS="${DROP_DISABLED_CHANNEL_ITEMS:-0}"

# If enabled, set Embeddings-DB-* groups mode to Failover.
SET_DB_EMBEDDING_GROUP_FAILOVER="${SET_DB_EMBEDDING_GROUP_FAILOVER:-1}"

# If enabled, apply a conservative embedding MatchRegex to groups with name containing "embed".
APPLY_EMBEDDING_REGEX="${APPLY_EMBEDDING_REGEX:-0}"
EMBEDDING_MATCH_REGEX_DEFAULT="${EMBEDDING_MATCH_REGEX_DEFAULT:-(?i)^(text-embedding-.*|.*embedding.*|(?:baai/)?bge-.*|netease-youdao/bce-embedding-base_v1|(?:qwen/)?qwen3-embedding-.*)$}"

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing dependency: $1" >&2; exit 1; }
}
require curl
require jq

log(){ printf "[%s] %s\n" "$(date -u +'%F %T UTC')" "$*" >&2; }

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

log "Fetching channels..."
channels_json="$(curl -sS "$BASE_URL/api/v1/channel/list" -H "Authorization: Bearer $ADMIN_JWT")"

# Map channel_id -> {type, enabled, name}
declare -A CH_TYPE
declare -A CH_ENABLED

declare -A CH_NAME

while IFS=$'\t' read -r id typ enabled name; do
  CH_TYPE["$id"]="$typ"
  CH_ENABLED["$id"]="$enabled"
  CH_NAME["$id"]="$name"
done < <(echo "$channels_json" | jq -r '.data[] | [.id, .type, .enabled, .name] | @tsv')

log "Fetching groups..."
groups_json="$(curl -sS "$BASE_URL/api/v1/group/list" -H "Authorization: Bearer $ADMIN_JWT")"

group_count="$(echo "$groups_json" | jq -r '.data | length')"
log "Groups: $group_count"

is_embedding_model() {
  local m="$1"
  m="${m,,}"
  if [[ "$m" == *rerank* ]]; then
    return 1
  fi
  [[ "$m" == text-embedding-* ]] && return 0
  [[ "$m" == *embedding* ]] && return 0
  [[ "$m" == bge-* ]] && return 0
  [[ "$m" == *"/bge-"* ]] && return 0
  [[ "$m" == *"qwen"* && "$m" == *"embedding"* ]] && return 0
  [[ "$m" == *"e5"* && "$m" == *"embed"* ]] && return 0
  return 1
}

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

log "Scanning groups (APPLY=$APPLY)..."

echo "$groups_json" | jq -c '.data[]' | while read -r g; do
  gid="$(echo "$g" | jq -r '.id')"
  gname="$(echo "$g" | jq -r '.name')"
  gmode="$(echo "$g" | jq -r '.mode')"

  delete_ids=()

  while IFS=$'\t' read -r item_id channel_id model_name; do
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

  done < <(echo "$g" | jq -r '.items[]? | [.id, .channel_id, .model_name] | @tsv')

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
    # show what we're deleting
    for id in "${delete_ids[@]}"; do
      item="$(echo "$g" | jq -c --argjson id "$id" '.items[] | select(.id==$id)')"
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

  payload="$(jq -n \
    --argjson id "$gid" \
    --argjson mode "${next_mode:-null}" \
    --arg match_regex "${next_match_regex:-}" \
    --argjson del "$(printf '%s\n' "${delete_ids[@]:-}" | jq -Rsc 'split("\n")[:-1] | map(tonumber)')" \
    '{id:$id}
      + (if $mode==null then {} else {mode:$mode} end)
      + (if $match_regex=="" then {} else {match_regex:$match_regex} end)
      + (if ($del|length)==0 then {} else {items_to_delete:$del} end)'
  )"

  apply_group_update "$payload"

done

log "Done. APPLY=$APPLY"
