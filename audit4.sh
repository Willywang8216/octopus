set -euo pipefail

# ---------- Defaults (override by env) ----------
BASE_URL="${BASE_URL:-http://172.18.0.39:8080}"
ADMIN_USER="${ADMIN_USER:-willywang8216+1gqw8a8n@gmail.com}"
ADMIN_PASS="${ADMIN_PASS:-1o@X9hJV!9w%J*Jp*niFW}"

CHANNEL_NAME_REGEX="${CHANNEL_NAME_REGEX:-.*}"

# 0 = only diagnose; 1 = may update keys/base_url
AUTO_DISABLE_BAD_KEYS="${AUTO_DISABLE_BAD_KEYS:-0}"
AUTO_FIX_BASE_URL="${AUTO_FIX_BASE_URL:-1}"
MAX_KEY_DISABLE="${MAX_KEY_DISABLE:-3}"

# cleanup pinned groups at end
CLEANUP_GROUPS="${CLEANUP_GROUPS:-0}"

PREFERRED_CHAT_MODELS="${PREFERRED_CHAT_MODELS:-gpt-5,gpt-5-mini,gpt-4o-mini,gpt-4.1-mini,deepseek-r1,deepseek-chat,claude-3-5-haiku-20241022,claude-3-5-sonnet-20241022,gemini-2.0-flash}"
PREFERRED_EMBED_MODELS="${PREFERRED_EMBED_MODELS:-text-embedding-3-small,text-embedding-3-large,BAAI/bge-m3,BAAI/bge-large-zh-v1.5,BAAI/bge-large-en-v1.5,netease-youdao/bce-embedding-base_v1}"

TEST_PROMPT="${TEST_PROMPT:-ping}"
TEST_EMBED_INPUT="${TEST_EMBED_INPUT:-ping}"

log(){ printf "\n[%s] %s\n" "$(date -u +'%F %T UTC')" "$*" >&2; }

require_env() {
  if [[ -z "${ADMIN_USER}" || -z "${ADMIN_PASS}" ]]; then
    log "ERROR: ADMIN_USER / ADMIN_PASS is empty. Put them in octopus.local.env and source it."
    exit 1
  fi
}

ADMIN_JWT=""

admin_login() {
  local response
  # 使用單引號包裹變數，確保特殊字元 ! % 不會被 Shell 解析
  response=$(curl -sS "$BASE_URL/api/v1/user/login" \
      -H "Content-Type: application/json" \
      -d "$(jq -n --arg u "$ADMIN_USER" --arg p "$ADMIN_PASS" '{"username":$u,"password":$p}')")
  
  ADMIN_JWT=$(echo "$response" | jq -r '.data.token // .data // empty')

  if [[ -z "$ADMIN_JWT" || "$ADMIN_JWT" == "null" ]]; then
    log "ERROR: admin login failed. API Response: $response"
    exit 1
  fi
}

api_get() { curl -sS "$BASE_URL$1" -H "Authorization: Bearer $ADMIN_JWT"; }
api_post_json() { curl -sS "$BASE_URL$1" -H "Authorization: Bearer $ADMIN_JWT" -H "Content-Type: application/json" -d "$2"; }
api_delete() { curl -sS -X DELETE "$BASE_URL$1" -H "Authorization: Bearer $ADMIN_JWT"; }

normalize_base_url() {
  local u="$1"
  u="${u%/}"
  u="${u%/chat/completions}"
  u="${u%/responses}"
  u="${u%/messages}"
  u="${u%/embeddings}"
  echo "$u"
}

split_models(){ echo "$1" | tr ',' '\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | awk 'NF>0'; }

choose_test_model() {
  local channel_models="$1"
  local preferred_csv="$2"
  local m
  while read -r m; do
    [[ -z "$m" ]] && continue
    if echo "$channel_models" | tr ',' '\n' | grep -i -x -F "$m" >/dev/null 2>&1; then
      echo "$m"; return 0
    fi
  done < <(split_models "$preferred_csv")
  split_models "$channel_models" | head -n 1
}

# mirror GetChannelKey(): enabled + not recent 429 + lowest total_cost
pick_selected_key_id() {
  local keys_json="$1"
  local now
  now="$(date +%s)"
  echo "$keys_json" | jq -r --argjson now "$now" '
    map(select(.enabled==true and (.channel_key|length)>0))
    | map(select((.status_code != 429) or (.last_use_time_stamp==0) or (($now - .last_use_time_stamp) >= 300)))
    | sort_by(.total_cost)
    | .[0].id // empty
  '
}

get_latest_log_for_group() {
  local grp="$1"
  local now start
  now="$(date +%s)"; start=$((now-1200))
  # try a few pages; cache is newest-first
  for page in 1 2 3 4 5; do
    api_get "/api/v1/log/list?page=$page&page_size=100&start_time=$start&end_time=$now" \
    | jq -c --arg g "$grp" '.data[] | select(.request_model_name==$g)' \
    | head -n 1 && return 0
  done
  return 1
}

ensure_audit_key() {
  api_get "/api/v1/apikey/list" > /tmp/apikeys.json
  local k
  k="$(jq -r '.data[] | select(.name=="__audit__") | .api_key' /tmp/apikeys.json | head -n 1)"
  if [[ -z "$k" || "$k" == "null" ]]; then
    log "No __audit__ key found. Creating..."
    local out
    out="$(api_post_json "/api/v1/apikey/create" '{"name":"__audit__","enabled":true}')"
    echo "$out" | jq -e '.code==200' >/dev/null || { echo "$out"; exit 1; }
    k="$(echo "$out" | jq -r '.data.api_key')"
  fi

  # verify
  if ! curl -sS -i "$BASE_URL/api/v1/apikey/login" -H "Authorization: Bearer $k" | head -n 1 | grep -q " 200 "; then
    log "ERROR: __audit__ key cannot pass /api/v1/apikey/login. Stop."
    exit 1
  fi
  echo "$k"
}

create_or_replace_group() {
  local grp="$1" cid="$2" model="$3"
  api_get "/api/v1/group/list" > /tmp/groups.json
  local gid
  gid="$(jq -r --arg n "$grp" '.data[]|select(.name==$n)|.id' /tmp/groups.json | head -n 1)"
  if [[ -n "$gid" && "$gid" != "null" ]]; then
    api_delete "/api/v1/group/delete/$gid" >/dev/null || true
  fi
  local out
  out="$(api_post_json "/api/v1/group/create" "$(jq -n --arg n "$grp" --arg m "$model" --argjson cid "$cid" '{
    name:$n, mode:3, match_regex:"",
    items:[{channel_id:$cid,model_name:$m,priority:1,weight:1}]
  }')")"
  echo "$out" | jq -e '.code==200' >/dev/null || { echo "$out"; exit 1; }
}

call_v1_chat() {
  local oct_key="$1" grp="$2"
  curl -sS -i "$BASE_URL/v1/chat/completions" \
    -H "Authorization: Bearer $oct_key" \
    -H "Content-Type: application/json" \
    -d "$(jq -n --arg m "$grp" --arg t "$TEST_PROMPT" '{model:$m,stream:false,messages:[{role:"user",content:$t}]}')"
}

call_v1_embed() {
  local oct_key="$1" grp="$2"
  curl -sS -i "$BASE_URL/v1/embeddings" \
    -H "Authorization: Bearer $oct_key" \
    -H "Content-Type: application/json" \
    -d "$(jq -n --arg m "$grp" --arg t "$TEST_EMBED_INPUT" '{model:$m,input:$t}')"
}

require_env
admin_login

log "Backup export..."
ts="$(date -u +%Y%m%d%H%M%S)"
curl -sS "$BASE_URL/api/v1/setting/export?include_logs=false&include_stats=false" \
  -H "Authorization: Bearer $ADMIN_JWT" > "octopus-export-$ts.json"
log "Backup: octopus-export-$ts.json"

log "Enable relay log keep..."
api_post_json "/api/v1/setting/set" '{"key":"relay_log_keep_enabled","value":"true"}' >/dev/null || true

OCT_KEY="$(ensure_audit_key)"
log "__audit__ key ready (not printing)."

log "Load channels..."
api_get "/api/v1/channel/list" > /tmp/channels.json
mapfile -t IDS < <(jq -r --arg re "$CHANNEL_NAME_REGEX" '.data[] | select(.name|test($re;"i")) | .id' /tmp/channels.json)
log "Channels matched: ${#IDS[@]}"

ok_count=0
fail_count=0

for cid in "${IDS[@]}"; do
  name="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.name' /tmp/channels.json)"
  ctype="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.type' /tmp/channels.json)"
  enabled="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.enabled' /tmp/channels.json)"
  base="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|(.base_urls|sort_by(.delay)|.[0].url // "")' /tmp/channels.json)"
  models="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|(.model + "," + (.custom_model//""))' /tmp/channels.json)"
  keys="$(jq -c --argjson id "$cid" '.data[]|select(.id==$id)|(.keys // [])' /tmp/channels.json)"

  log "=== Channel id=$cid name=$name type=$ctype enabled=$enabled ==="

  if [[ "$AUTO_FIX_BASE_URL" == "1" && -n "$base" ]]; then
    norm="$(normalize_base_url "$base")"
    if [[ "$norm" != "$base" ]]; then
      log "Fix base_url: $base -> $norm"
      api_post_json "/api/v1/channel/update" "$(jq -n --argjson id "$cid" --arg u "$norm" '{id:$id,base_urls:[{url:$u,delay:0}] }')" >/dev/null || true
      # reload snapshot for this channel
      api_get "/api/v1/channel/list" > /tmp/channels.json
      base="$norm"
      keys="$(jq -c --argjson id "$cid" '.data[]|select(.id==$id)|(.keys // [])' /tmp/channels.json)"
    fi
  fi

  if [[ "$ctype" == "5" ]]; then
    test_model="$(choose_test_model "$models" "$PREFERRED_EMBED_MODELS")"
  else
    test_model="$(choose_test_model "$models" "$PREFERRED_CHAT_MODELS")"
  fi

  if [[ -z "$test_model" ]]; then
    log "SKIP: no model configured"
    continue
  fi

  grp="__audit__ch_${cid}"
  create_or_replace_group "$grp" "$cid" "$test_model"

  # retry loop: allow disabling bad keys on 401/403
  disabled=0
  while true; do
    if [[ "$ctype" == "5" ]]; then
      resp="$(call_v1_embed "$OCT_KEY" "$grp" || true)"
    else
      resp="$(call_v1_chat "$OCT_KEY" "$grp" || true)"
    fi
    status="$(echo "$resp" | head -n 1 | awk '{print $2}')"
    if [[ "$status" == "200" ]]; then
      log "OK"
      ok_count=$((ok_count+1))
      break
    fi

    fail_count=$((fail_count+1))
    log "FAIL http=$status (fetch relay log for details...)"

    if logline="$(get_latest_log_for_group "$grp")"; then
      echo "$logline" | jq -r '
        "log: time=\(.time) channel=\(.channel_name) model=\(.actual_model_name) use_ms=\(.use_time)\nerror=\(.error)\n"
      '
      err="$(echo "$logline" | jq -r '.error // ""')"

      if [[ "$AUTO_DISABLE_BAD_KEYS" == "1" ]] && echo "$err" | grep -E 'upstream error: (401|403):' >/dev/null 2>&1; then
        kid="$(pick_selected_key_id "$keys")"
        if [[ -z "$kid" ]]; then
          log "No selectable key to disable. Stop retry."
          break
        fi
        disabled=$((disabled+1))
        if [[ "$disabled" -gt "$MAX_KEY_DISABLE" ]]; then
          log "Reached MAX_KEY_DISABLE=$MAX_KEY_DISABLE. Stop retry."
          break
        fi
        log "Auto-disable key_id=$kid on channel $cid ($name) and retry..."
        api_post_json "/api/v1/channel/update" "$(jq -n --argjson id "$cid" --argjson kid "$kid" '{
          id:$id,
          keys_to_update:[{id:$kid, enabled:false, remark:"auto-disabled by audit4.sh (upstream auth failure)"}]
        }')" >/dev/null || true
        api_get "/api/v1/channel/list" > /tmp/channels.json
        keys="$(jq -c --argjson id "$cid" '.data[]|select(.id==$id)|(.keys // [])' /tmp/channels.json)"
        continue
      fi
    else
      log "No relay log found for $grp yet."
    fi

    break
  done

  if [[ "$CLEANUP_GROUPS" == "1" ]]; then
    api_get "/api/v1/group/list" > /tmp/groups.json
    gid="$(jq -r --arg n "$grp" '.data[]|select(.name==$n)|.id' /tmp/groups.json | head -n 1)"
    if [[ -n "$gid" && "$gid" != "null" ]]; then
      api_delete "/api/v1/group/delete/$gid" >/dev/null || true
    fi
  fi
done

log "SUMMARY: ok=$ok_count fail=$fail_count"
