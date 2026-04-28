#!/usr/bin/env bash
# ============================================================================
# Octopus Group Redesign Script
#
# Deletes all existing groups and recreates them with the correct design:
# one group per model name (matching what clients request), plus alias
# groups for convenience routing.
#
# Usage:
#   export OCTOPUS_URL="http://localhost:8080"
#   export OCTOPUS_TOKEN="your-jwt-token"
#   bash scripts/redesign-groups.sh
# ============================================================================

set -euo pipefail

API="${OCTOPUS_URL:-http://localhost:8080}"
TOKEN="${OCTOPUS_TOKEN:-}"

if [ -z "$TOKEN" ]; then
  echo "ERROR: Set OCTOPUS_TOKEN to your JWT auth token."
  exit 1
fi

AUTH="Authorization: Bearer $TOKEN"
CT="Content-Type: application/json"

create_group() {
  local name="$1" mode="$2" regex="${3:-}" ftt="${4:-0}" skt="${5:-0}"
  local body="{\"name\":\"$name\",\"mode\":$mode,\"match_regex\":\"$regex\",\"first_token_time_out\":$ftt,\"session_keep_time\":$skt}"
  local code
  code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API/api/v1/group/create" -H "$AUTH" -H "$CT" -d "$body")
  [ "$code" = "200" ] && echo "  [+] $name" || echo "  [~] skip: $name"
}

echo "=== Step 1: Delete all existing groups ==="
curl -s "$API/api/v1/group/list" -H "$AUTH" | python3 -c "
import sys,json
data=json.load(sys.stdin)
gs=data if isinstance(data,list) else data.get('data',[])
for g in gs: print(g['id'],g['name'])
" 2>/dev/null | while read -r gid gname; do
  curl -s -X DELETE "$API/api/v1/group/delete/$gid" -H "$AUTH" > /dev/null
  echo "  [-] $gname (id=$gid)"
done

echo ""
echo "=== Step 2: Create 1:1 model groups (RoundRobin) ==="

for m in \
  qwen/qwen3-8b qwen/qwen3-14b qwen/qwen3-32b qwen/qwen3-30b-a3b \
  qwen/qwen3-235b-a22b qwen/qwen3.5-9b qwen/qwen3.5-27b qwen/qwen3.5-35b-a3b \
  qwen/qwen3.5-122b-a10b qwen/qwen3.5-397b-a17b qwen/qwen3.6-35b-a3b \
  qwen/qwen3-30b-a3b-instruct-2507 qwen/qwen3-30b-a3b-thinking-2507 \
  qwen/qwen3-235b-a22b-instruct-2507 qwen/qwen3-235b-a22b-thinking-2507 \
  qwen/qwen3-next-80b-a3b-instruct qwen/qwen3-next-80b-a3b-thinking \
  qwen/qwq-32b \
  qwen/qwen2.5-coder-32b-instruct qwen/qwen3-coder-30b-a3b-instruct \
  qwen/qwen3-coder-480b-a35b-instruct \
  qwen/qwen2.5-vl-32b-instruct qwen/qwen2.5-vl-72b-instruct \
  qwen/qwen3-vl-8b-instruct qwen/qwen3-vl-8b-thinking \
  qwen/qwen3-vl-32b-instruct qwen/qwen3-vl-32b-thinking \
  qwen/qwen3-vl-30b-a3b-instruct qwen/qwen3-vl-30b-a3b-thinking \
  qwen/qwen3-vl-235b-a22b-instruct \
  qwen/qwen3-omni-30b-a3b-instruct qwen/qwen3-omni-30b-a3b-thinking \
  qwen/qwen3-omni-30b-a3b-captioner \
  qwen/qwen3-embedding-0.6b qwen/qwen3-embedding-4b qwen/qwen3-embedding-8b \
  qwen/qwen3-vl-embedding-8b \
  qwen/qwen3-reranker-0.6b qwen/qwen3-reranker-4b qwen/qwen3-reranker-8b \
  qwen1.5-0.5b-chat qwen1.5-1.8b-chat qwen1.5-14b-chat \
  qwen1.5-32b-chat qwen1.5-110b-chat \
; do
  create_group "$m" 1
done

echo ""
echo "=== Step 3: Create alias groups ==="
create_group "agentic-coder"  3 "" 60 300
create_group "best-reasoning" 3 "" 30 0
create_group "best-chat"      3 "" 0  0
create_group "fast-chat"      1 "" 0  0
create_group "best-vision"    3 "" 0  0
create_group "best-embedding" 3 "" 0  0

echo ""
echo "=== Done ==="
echo "Now enable auto_group=Exact on your channels so items auto-populate."
echo ""
echo "For alias groups, add items via UI with this priority order:"
echo "  agentic-coder:  480b-a35b(1) > 30b-a3b(2) > 2.5-coder-32b(3)"
echo "  best-reasoning: qwq-32b(1) > 235b-thinking-2507(2) > next-80b-thinking(3)"
echo "  best-chat:      3.5-397b(1) > 3.5-122b(2) > 3-235b(3)"
echo "  fast-chat:      3-8b(1) > 3.5-9b(2) > 3-30b-a3b(3)"
echo "  best-vision:    vl-235b(1) > vl-32b(2) > 2.5-vl-72b(3)"
echo "  best-embedding: emb-8b(1) > emb-4b(2) > emb-0.6b(3)"
