#!/usr/bin/env bash
set -euo pipefail

#############################################
# Config (set via env vars)
#############################################
BASE_URL="${BASE_URL:-http://172.18.0.39:8080}"      # Prefer local origin for admin API + v1 tests
ADMIN_USER="${ADMIN_USER:-willywang8216+1gqw8a8n@gmail.com}"
ADMIN_PASS="${ADMIN_PASS:-1o@X9hJV!9w%J*Jp*niFW}"

# Only process channel names matching regex (case-insensitive). Default: all.
CHANNEL_NAME_REGEX="${CHANNEL_NAME_REGEX:-.*}"

# If 1, do not apply changes (no key disabling, no base_url updates).
DRY_RUN="${DRY_RUN:-1}"

# If 1, delete __audit__ groups after running.
CLEANUP_GROUPS="${CLEANUP_GROUPS:-0}"

# Max number of failing keys to disable per channel before giving up.
MAX_KEY_DISABLE="${MAX_KEY_DISABLE:-3}"

# Candidate models to test (comma-separated). Script will choose the first that exists in channel model list.
PREFERRED_CHAT_MODELS="${PREFERRED_CHAT_MODELS:-gpt-5,gpt-5-mini,gpt-4o-mini,gpt-4.1-mini,claude-3-5-sonnet,gemini-2.0-flash,deepseek-r1}"
PREFERRED_EMBED_MODELS="${PREFERRED_EMBED_MODELS:-text-embedding-3-small,text-embedding-3-large,BAAI/bge-m3,BAAI/bge-large-zh-v1.5,BAAI/bge-large-en-v1.5,netease-youdao/bce-embedding-base_v1}"

TEST_PROMPT="${TEST_PROMPT:-ping}"
TEST_EMBED_INPUT="${TEST_EMBED_INPUT:-ping}"

#############################################
# Helpers
#############################################
log() { printf "\n[%s] %s\n" "$(date -u +'%F %T UTC')" "$*" >&2; }

ADMIN_JWT=""
AUDIT_OCT_KEY=""

api_login() {
  log "Admin login..."
  ADMIN_JWT="$(
    curl -sS "$BASE_URL/api/v1/user/login" \
      -H "Content-Type: application/json" \
      -d "$(jq -n --arg u "$ADMIN_USER" --arg p "$ADMIN_PASS" '{username:$u,password:$p}')" \
    | jq -r '.data.token // empty'
  )"
  if [[ -z "$ADMIN_JWT" ]]; then
    log "ERROR: admin login failed (no token)."
    exit 1
  fi
}

api_get() {
  local path="$1"
  local out
  out="$(curl -sS "$BASE_URL$path" -H "Authorization: Bearer $ADMIN_JWT" || true)"
  if echo "$out" | jq -e '.code==401' >/dev/null 2>&1; then
    api_login
    out="$(curl -sS "$BASE_URL$path" -H "Authorization: Bearer $ADMIN_JWT")"
  fi
  echo "$out"
}

api_post_json() {
  local path="$1"
  local json="$2"
  local out
  out="$(curl -sS "$BASE_URL$path" \
    -H "Authorization: Bearer $ADMIN_JWT" \
    -H "Content-Type: application/json" \
    -d "$json" || true)"
  if echo "$out" | jq -e '.code==401' >/dev/null 2>&1; then
    api_login
    out="$(curl -sS "$BASE_URL$path" \
      -H "Authorization: Bearer $ADMIN_JWT" \
      -H "Content-Type: application/json" \
      -d "$json")"
  fi
  echo "$out"
}

api_delete() {
  local path="$1"
  local out
  out="$(curl -sS -X DELETE "$BASE_URL$path" -H "Authorization: Bearer $ADMIN_JWT" || true)"
  if echo "$out" | jq -e '.code==401' >/dev/null 2>&1; then
    api_login
    out="$(curl -sS -X DELETE "$BASE_URL$path" -H "Authorization: Bearer $ADMIN_JWT")"
  fi
  echo "$out"
}

# Normalize common “base_url includes endpoint” mistakes.
normalize_base_url() {
  local u="$1"
  u="${u%/}"
  u="${u%/chat/completions}"
  u="${u%/responses}"
  u="${u%/messages}"
  u="${u%/embeddings}"
  echo "$u"
}

split_models() {
  # input: "a,b,c" -> prints one per line trimmed
  echo "$1" | tr ',' '\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | awk 'NF>0'
}

choose_test_model() {
  local channel_models="$1"         # comma list
  local preferred_csv="$2"          # comma list
  local pref
  while read -r pref; do
    if [[ -z "$pref" ]]; then continue; fi
    if echo "$channel_models" | tr ',' '\n' | grep -i -x -F "$pref" >/dev/null 2>&1; then
      echo "$pref"
      return 0
    fi
  done < <(split_models "$preferred_csv")
  # fallback: first channel model
  split_models "$channel_models" | head -n 1
}

# Compute which key Octopus would pick (mirrors Channel.GetChannelKey selection rules).
pick_selected_key_id() {
  local keys_json="$1"  # JSON array
  local now
  now="$(date +%s)"
  echo "$keys_json" | jq -r --argjson now "$now" '
    map(select(.enabled==true and (.channel_key|length)>0))
    | map(select(
        (.status_code != 429) or
        (.last_use_time_stamp==0) or
        (($now - .last_use_time_stamp) >= 300)
      ))
    | sort_by(.total_cost)
    | .[0].id // empty
  '
}

call_octopus_chat() {
  local model_group="$1"
  local body
  body="$(jq -n --arg m "$model_group" --arg t "$TEST_PROMPT" '{
    model:$m,
    stream:false,
    messages:[{role:"user",content:$t}],
    temperature:0.2
  }')"
  curl -sS -i "$BASE_URL/v1/chat/completions" \
    -H "Authorization: Bearer '"$AUDIT_OCT_KEY"'" \
    -H "Content-Type: application/json" \
    -d "$body"
}

call_octopus_embedding() {
  local model_group="$1"
  local body
  body="$(jq -n --arg m "$model_group" --arg t "$TEST_EMBED_INPUT" '{
    model:$m,
    input:$t
  }')"
  curl -sS -i "$BASE_URL/v1/embeddings" \
    -H "Authorization: Bearer '"$AUDIT_OCT_KEY"'" \
    -H "Content-Type: application/json" \
    -d "$body"
}

http_status_from_response() {
  # reads full HTTP response from stdin; prints status code
  head -n 1 | awk '{print $2}'
}

json_message_from_response() {
  # reads full HTTP response from stdin; prints .message if JSON, else empty
  awk 'BEGIN{body=0} /^\r?$/{body=1;next} {if(body) print}' \
    | jq -r '.message // empty' 2>/dev/null || true
}

#############################################
# Main
#############################################
api_login

log "Backing up config via /api/v1/setting/export ..."
ts="$(date -u +%Y%m%d%H%M%S)"
backup_file="octopus-export-${ts}.json"
# Note: this exports config; include_logs/stats false keeps it smaller
curl -sS "$BASE_URL/api/v1/setting/export?include_logs=false&include_stats=false" \
  -H "Authorization: Bearer $ADMIN_JWT" > "$backup_file"
log "Backup written: $backup_file"

log "Ensure relay logs enabled ..."
api_post_json "/api/v1/setting/set" '{"key":"relay_log_keep_enabled","value":"true"}' >/dev/null || true

log "Ensure an audit API key exists (name=__audit__) ..."
api_get "/api/v1/apikey/list" > /tmp/apikeys.json
AUDIT_OCT_KEY="$(jq -r '.data[] | select(.name=="__audit__") | .api_key' /tmp/apikeys.json | head -n 1)"
if [[ -z "$AUDIT_OCT_KEY" || "$AUDIT_OCT_KEY" == "null" ]]; then
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY_RUN=1: would create audit API key __audit__ (needed to call /v1/*)."
    log "Set DRY_RUN=0 to create it."
    exit 1
  fi
  out="$(api_post_json "/api/v1/apikey/create" '{"name":"__audit__","enabled":true}')"
  AUDIT_OCT_KEY="$(echo "$out" | jq -r '.data.api_key')"
  log "Created audit key."
fi
log "Audit key ready (not printing)."

log "Fetch channels ..."
api_get "/api/v1/channel/list" > /tmp/channels.json

# Filter channels by name regex
CHANNEL_IDS=($(jq -r --arg re "$CHANNEL_NAME_REGEX" '
  .data[] | select(.name|test($re;"i")) | .id
' /tmp/channels.json))

log "Channels matched: ${#CHANNEL_IDS[@]}"

# Cache group list for delete/create
api_get "/api/v1/group/list" > /tmp/groups.json

# Summary arrays
declare -a SUMMARY_OK SUMMARY_FAIL

for cid in "${CHANNEL_IDS[@]}"; do
  name="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.name' /tmp/channels.json)"
  ctype="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.type' /tmp/channels.json)"
  enabled="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.enabled' /tmp/channels.json)"
  base="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|(.base_urls|sort_by(.delay)|.[0].url // "")' /tmp/channels.json)"
  models="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|(.model + "," + (.custom_model//""))' /tmp/channels.json)"
  keys="$(jq -c --argjson id "$cid" '.data[]|select(.id==$id)|(.keys // [])' /tmp/channels.json)"

  log "=== Channel: id=$cid name=$name type=$ctype enabled=$enabled ==="

  if [[ "$enabled" != "true" ]]; then
    if [[ "$DRY_RUN" == "1" ]]; then
      log "DRY_RUN: would enable channel $cid ($name)"
    else
      api_post_json "/api/v1/channel/enable" "$(jq -n --argjson id "$cid" '{id:$id,enabled:true}')" >/dev/null
      log "Enabled."
    fi
  fi

  norm_base="$(normalize_base_url "$base")"
  if [[ -n "$base" && "$norm_base" != "$base" ]]; then
    if [[ "$DRY_RUN" == "1" ]]; then
      log "DRY_RUN: would normalize base_url: $base -> $norm_base"
    else
      api_post_json "/api/v1/channel/update" "$(jq -n --argjson id "$cid" --arg u "$norm_base" '{id:$id,base_urls:[{url:$u,delay:0}] }')" >/dev/null
      log "Updated base_url."
      # refresh cached base
      base="$norm_base"
    fi
  fi

  # Choose model
  if [[ "$ctype" == "5" ]]; then
    test_model="$(choose_test_model "$models" "$PREFERRED_EMBED_MODELS")"
  else
    test_model="$(choose_test_model "$models" "$PREFERRED_CHAT_MODELS")"
  fi

  if [[ -z "$test_model" ]]; then
    log "SKIP: no model found in channel.model/custom_model"
    SUMMARY_FAIL+=("$cid\t$name\tno models configured")
    continue
  fi
  log "Test model: $test_model"

  # Create pinned group
  grp="__audit__ch_${cid}"
  gid="$(jq -r --arg n "$grp" '.data[]|select(.name==$n)|.id' /tmp/groups.json | head -n 1)"

  if [[ -n "$gid" && "$gid" != "null" ]]; then
    if [[ "$DRY_RUN" == "1" ]]; then
      log "DRY_RUN: would delete existing audit group $grp (id=$gid)"
    else
      api_delete "/api/v1/group/delete/$gid" >/dev/null || true
      log "Deleted existing audit group."
    fi
  fi

  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY_RUN: would create group $grp pinned to channel=$cid model=$test_model"
  else
    out="$(api_post_json "/api/v1/group/create" "$(jq -n --arg n "$grp" --arg m "$test_model" --argjson cid "$cid" '{
      name:$n,
      mode:3,
      match_regex:"",
      items:[{channel_id:$cid,model_name:$m,priority:1,weight:1}]
    }')")"
    echo "$out" | jq -e '.code==200' >/dev/null || { echo "$out"; exit 1; }
    log "Created audit group $grp."
    # refresh groups cache incrementally (simpler: reload)
    api_get "/api/v1/group/list" > /tmp/groups.json
  fi

  # If dry-run, stop here (no v1 calls)
  if [[ "$DRY_RUN" == "1" ]]; then
    continue
  fi

  # Attempt call; on auth failures, disable selected key and retry
  attempt=0
  disabled=0
  ok=0
  last_msg=""

  while [[ "$attempt" -lt $((MAX_KEY_DISABLE+1)) ]]; do
    attempt=$((attempt+1))
    log "Octopus test attempt $attempt..."

    if [[ "$ctype" == "5" ]]; then
      resp="$(call_octopus_embedding "$grp")"
    else
      resp="$(call_octopus_chat "$grp")"
    fi

    status="$(printf "%s" "$resp" | http_status_from_response)"
    msg="$(printf "%s" "$resp" | json_message_from_response)"
    last_msg="$msg"

    if [[ "$status" == "200" ]]; then
      ok=1
      break
    fi

    log "FAIL: HTTP $status message=$msg"

    # Only auto-disable keys if error clearly indicates upstream auth
    if echo "$msg" | grep -E 'upstream error: (401|403):' >/dev/null 2>&1; then
      selected_key_id="$(pick_selected_key_id "$keys")"
      if [[ -z "$selected_key_id" ]]; then
        log "No selectable key found to disable."
        break
      fi
      disabled=$((disabled+1))
      if [[ "$disabled" -gt "$MAX_KEY_DISABLE" ]]; then
        log "Reached MAX_KEY_DISABLE=$MAX_KEY_DISABLE"
        break
      fi

      log "Disabling selected key_id=$selected_key_id for channel $cid ($name)"
      api_post_json "/api/v1/channel/update" "$(jq -n --argjson id "$cid" --argjson kid "$selected_key_id" '{
        id:$id,
        keys_to_update:[{id:$kid, enabled:false, remark:"auto-disabled by octopus_audit_fix.sh (auth failure)"}]
      }')" >/dev/null || true

      # refresh channel cache snapshot
      api_get "/api/v1/channel/list" > /tmp/channels.json
      keys="$(jq -c --argjson id "$cid" '.data[]|select(.id==$id)|(.keys // [])' /tmp/channels.json)"

      continue
    fi

    # If 404 or endpoint/path-like failure, do not disable keys; recommend manual review
    break
  done

  if [[ "$ok" == "1" ]]; then
    log "OK: channel $cid ($name) works via Octopus with group $grp"
    SUMMARY_OK+=("$cid\t$name\t$test_model")
  else
    log "FAILED: channel $cid ($name) last_message=$last_msg"
    SUMMARY_FAIL+=("$cid\t$name\t$last_msg")
  fi

  if [[ "$CLEANUP_GROUPS" == "1" ]]; then
    gid="$(jq -r --arg n "$grp" '.data[]|select(.name==$n)|.id' /tmp/groups.json | head -n 1)"
    if [[ -n "$gid" && "$gid" != "null" ]]; then
      api_delete "/api/v1/group/delete/$gid" >/dev/null || true
      api_get "/api/v1/group/list" > /tmp/groups.json
      log "Cleaned up group $grp"
    fi
  fi
done

log "===================="
log "SUMMARY (OK)"
printf "channel_id\tchannel_name\ttest_model\n"
printf "%s\n" "${SUMMARY_OK[@]:-}" || true

log "===================="
log "SUMMARY (FAILED)"
printf "channel_id\tchannel_name\tlast_error\n"
printf "%s\n" "${SUMMARY_FAIL[@]:-}" || true

log "Done. Backup: $backup_file"
