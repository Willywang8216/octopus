set -euo pipefail

BASE_URL="${BASE_URL:-http://172.18.0.39:8080}"
ADMIN_USER="${ADMIN_USER:-willywang8216+1gqw8a8n@gmail.com}"
ADMIN_PASS="${ADMIN_PASS:-1o@X9hJV!9w%J*Jp*niFW}"

CHANNEL_NAME_REGEX="${CHANNEL_NAME_REGEX:-.*}"
DRY_RUN="${DRY_RUN:-0}"

# 优先使用你手动提供的 OCT_KEY（建议），避免多实例 cache 问题
OCT_KEY="${OCT_KEY:-}"

log(){ printf "\n[%s] %s\n" "$(date -u +'%F %T UTC')" "$*" >&2; }

admin_login() {
  ADMIN_JWT="$(
    curl -sS "$BASE_URL/api/v1/user/login" \
      -H "Content-Type: application/json" \
      -d "$(jq -n --arg u "$ADMIN_USER" --arg p "$ADMIN_PASS" '{username:$u,password:$p}')" \
    | jq -r '.data.token // empty'
  )"
  [[ -n "$ADMIN_JWT" ]] || { log "ERROR: admin login failed"; exit 1; }
}

api_get() {
  curl -sS "$BASE_URL$1" -H "Authorization: Bearer $ADMIN_JWT"
}

api_post() {
  curl -sS "$BASE_URL$1" -H "Authorization: Bearer $ADMIN_JWT" -H "Content-Type: application/json" -d "$2"
}

mask_key() {
  local k="$1"
  if [[ -z "$k" || "$k" == "null" ]]; then echo "<empty>"; return; fi
  echo "${k:0:14}..."
}

verify_oct_key() {
  local k="$1"
  if [[ -z "$k" || "$k" == "null" ]]; then
    return 1
  fi
  # 验证 /v1 认证层
  local out
  out="$(curl -sS -i "$BASE_URL/api/v1/apikey/login" -H "Authorization: Bearer $k" || true)"
  echo "$out" | head -n 1 | grep -q " 200 " && return 0
  return 1
}

admin_login

# 1) 选用 OCT_KEY
if [[ -n "$OCT_KEY" ]]; then
  log "Using OCT_KEY from env: $(mask_key "$OCT_KEY")"
  if ! verify_oct_key "$OCT_KEY"; then
    log "ERROR: OCT_KEY failed /api/v1/apikey/login. Stop."
    exit 1
  fi
else
  log "OCT_KEY not provided. Looking for __audit__ key..."
  api_get "/api/v1/apikey/list" > /tmp/apikeys.json
  OCT_KEY="$(jq -r '.data[] | select(.name=="__audit__") | .api_key' /tmp/apikeys.json | head -n 1)"
  log "Found __audit__ key: $(mask_key "$OCT_KEY")"
  if ! verify_oct_key "$OCT_KEY"; then
    log "ERROR: __audit__ key cannot pass /api/v1/apikey/login."
    log "Fix: set OCT_KEY=your-known-working sk-octopus-... and rerun."
    exit 1
  fi
fi

log "OK: Octopus /v1 authentication works. Now auditing channels..."

# 2) Fetch channels
api_get "/api/v1/channel/list" > /tmp/channels.json
mapfile -t IDS < <(jq -r --arg re "$CHANNEL_NAME_REGEX" '.data[] | select(.name|test($re;"i")) | .id' /tmp/channels.json)

log "Channels matched: ${#IDS[@]}"

# 3) For each channel, create pinned group and call /v1/chat/completions
for cid in "${IDS[@]}"; do
  name="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.name' /tmp/channels.json)"
  ctype="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.type' /tmp/channels.json)"
  enabled="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|.enabled' /tmp/channels.json)"
  models="$(jq -r --argjson id "$cid" '.data[]|select(.id==$id)|(.model + "," + (.custom_model//""))' /tmp/channels.json)"

  log "=== Channel id=$cid name=$name type=$ctype enabled=$enabled ==="

  # Pick the first model configured on that channel (you can improve selection later)
  test_model="$(echo "$models" | tr ',' '\n' | sed 's/^[ ]*//;s/[ ]*$//' | awk 'NF{print;exit}')"
  if [[ -z "$test_model" ]]; then
    log "SKIP: no model configured"
    continue
  fi

  grp="__audit__ch_${cid}"

  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY_RUN: would create group=$grp -> channel=$cid model=$test_model"
    continue
  fi

  # delete existing group if exists
  api_get "/api/v1/group/list" > /tmp/groups.json
  gid="$(jq -r --arg n "$grp" '.data[]|select(.name==$n)|.id' /tmp/groups.json | head -n 1)"
  if [[ -n "$gid" && "$gid" != "null" ]]; then
    curl -sS -X DELETE "$BASE_URL/api/v1/group/delete/$gid" -H "Authorization: Bearer $ADMIN_JWT" >/dev/null || true
  fi

  # create group pinned to this channel/model
  out="$(api_post "/api/v1/group/create" "$(jq -n --arg n "$grp" --arg m "$test_model" --argjson cid "$cid" '{
    name:$n, mode:3, match_regex:"",
    items:[{channel_id:$cid, model_name:$m, priority:1, weight:1}]
  }')")"
  echo "$out" | jq -e '.code==200' >/dev/null || { echo "$out"; exit 1; }

  # call /v1 based on type
  if [[ "$ctype" == "5" ]]; then
    resp="$(curl -sS -i "$BASE_URL/v1/embeddings" \
      -H "Authorization: Bearer $OCT_KEY" \
      -H "Content-Type: application/json" \
      -d "$(jq -n --arg m "$grp" '{model:$m,input:"ping"}')" || true)"
  else
    resp="$(curl -sS -i "$BASE_URL/v1/chat/completions" \
      -H "Authorization: Bearer $OCT_KEY" \
      -H "Content-Type: application/json" \
      -d "$(jq -n --arg m "$grp" '{model:$m,stream:false,messages:[{role:"user",content:"ping"}] }')" || true)"
  fi

  status="$(echo "$resp" | head -n 1 | awk '{print $2}')"
  msg="$(echo "$resp" | awk 'BEGIN{b=0} /^\r?$/{b=1;next} {if(b)print}' | jq -r '.message // empty' 2>/dev/null || true)"

  log "Result: http=$status message=$msg"
done
