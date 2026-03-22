set -euo pipefail

BASE_URL="https://llmapi.iamwillywang.com"

# 1) 登录拿 JWT（不要把密码留在 shell history；建议改成 read -s）
ADMIN_JWT=$(
  curl -sS "$BASE_URL/api/v1/user/login" \
    -H "Content-Type: application/json" \
    -d '{"username":"REDACTED","password":"REDACTED"}' \
  | jq -r '.data.token'
)

# 2) 通过渠道名称找 WenRouter 的 id（你这里实际是 19）
WEN_ID=$(
  curl -sS "$BASE_URL/api/v1/channel/list" \
    -H "Authorization: Bearer $ADMIN_JWT" \
  | jq -r '.data[] | select(.name|test("^WenRouter$";"i")) | .id'
)
echo "WEN_ID=$WEN_ID"
test -n "$WEN_ID"

# 3) 创建 group（如果已存在会报错；见 4.2）
cat > wenrouter-test.group.json <<EOF
{
  "name": "WenRouter-Test",
  "mode": 3,
  "match_regex": "",
  "items": [
    { "channel_id": $WEN_ID, "model_name": "gpt-5", "priority": 1, "weight": 1 }
  ]
}
EOF

curl -sS "$BASE_URL/api/v1/group/create" \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H "Content-Type: application/json" \
  -d @wenrouter-test.group.json \
| jq

# 4) 用 Octopus API key 验证 /v1/models 是否能看到 WenRouter-Test
OCT_KEY="sk-octopus-Le5NkSz8O5ypKXrhS4LdQgU3PJG9ly48scqcxDKYzCRPDAaV"

curl -sS "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $OCT_KEY" \
| jq -r '.data[].id' | grep -E '^WenRouter-Test$' && echo "Model visible"

# 5) 调用 WenRouter-Test（non-stream）
curl -sS "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $OCT_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"WenRouter-Test","messages":[{"role":"user","content":"ping"}]}' \
| jq -r '.model, .choices[0].message.content'
