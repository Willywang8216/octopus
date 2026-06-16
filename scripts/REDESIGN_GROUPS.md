# Octopus Group Taxonomy

This is the live taxonomy after the full 2026-04-28 revamp. Source of truth
is `scripts/redesignGroups.py` and `scripts/auditChannels.py`. This doc is the
human-readable reference.

The system carries 600+ models across 15+ providers (OpenAI, Anthropic,
DeepSeek, GLM/Zhipu, Qwen/Alibaba, MiniMax, Moonshot/Kimi, Doubao, Llama,
Mistral, Gemini, Grok, LongCat, etc). The taxonomy is intentionally
**provider-agnostic** and **capability-first**. Callers ask for a
*capability* (e.g. `code`, `vision`); Octopus picks a model from the best
available channels at request time.

## TL;DR — pick a group by what you're doing

| Workflow | Group |
|---|---|
| Default chat / Q&A | `chat` |
| Hardest task, money no object | `chat-flagship` |
| Bulk processing, classification, extraction | `chat-fast` |
| Code gen, agentic coding (Cline, Aider, Cursor) | `code` |
| Math, planning, deep analysis | `reason` |
| Image understanding, OCR, document analysis | `vision` |
| Hard visual reasoning, math from photos | `vision-thinking` |
| Text-to-image, image editing | `image-gen` |
| Text-to-video | `video-gen` |
| Speech-to-text, text-to-speech | `audio` |
| Reranking RAG retrieval (`/v1/rerank`) | `rerank` |
| One-off embedding (NOT for stored vectors) | `embed-adhoc` |
| **RAG: embed text into / query a vector DB** | `Embeddings-DB-MULTI-BGE-M3` (default) |

## How to call a group

```sh
curl https://llmapi.iamwillywang.com/v1/chat/completions \
  -H "Authorization: Bearer sk-octopus-..." \
  -H "Content-Type: application/json" \
  -d '{"model":"chat","messages":[{"role":"user","content":"hi"}]}'
```

The `model` field must be one of the group names listed above. Octopus resolves
the group, picks a high-quality channel, and forwards to the upstream's actual
model. Old-style direct model names like `claude-opus-4-6` are not entered as
group names — they live as items inside groups.

## Embeddings — the vector-DB rule

**Once data is embedded into a vector database with model X, every future
query against that data must use exactly the same model X.** Different
embedding models produce vectors in different spaces with different
dimensionality and geometry — similarity scores between them are meaningless.

### DB-bound embedding groups

These groups route to exactly one embedding model so callers always get
vectors in the correct space. **Names are immutable contracts** — code that
refers to them won't break across config changes.

| Group | Model | Dim | Use when |
|---|---|---|---|
| `Embeddings-DB-MULTI-BGE-M3` | `BAAI/bge-m3` | 1024 | Mixed/multilingual content. **Default for new RAG.** |
| `Embeddings-DB-EN-BGE-Large` | `BAAI/bge-large-en-v1.5` | 1024 | English-only existing index |
| `Embeddings-DB-ZH-BGE-Large` | `BAAI/bge-large-zh-v1.5` | 1024 | Chinese-only existing index |

**Why BGE-M3 by default**: 100+ languages, 8192-token context (vs. 512 for
BGE-Large), supports dense + sparse + ColBERT retrieval, all 1024 dimensions.
Future-proof for whatever data you throw at it.

**Never use `embed-adhoc` for stored vectors.** It round-robins across
multiple embedding model families with different dimensions (Qwen3=4096,
BCE=768, NVIDIA NV-Embed=4096, BGE=1024). Two queries of the same text could
land on different models and produce incompatible vectors.

## Routing modes

| Mode | Used for | Behaviour |
|---|---|---|
| **Failover** (3) | Most groups | Try priority-1 item first; advance on failure |
| **RoundRobin** (1) | `chat-fast`, `embed-adhoc`, `rerank` | Cycle items at the same priority |

Within each group, items get a `priority` value combining a base (per the
target taxonomy) and a quality offset (per the channel's audited band):

| Channel band | Priority offset | Effect |
|---|---|---|
| `[ALIVE]` | +0 | preferred |
| `[NEW]` | +50 | tried after proven channels |
| `[FLAKY]` | +100 | last resort |
| `[DEAD]` / `[ZOMBIE]` | excluded | not added to groups at all |

## Channel quality tags

Every channel name carries a `[BAND]` prefix from the most recent audit
(`scripts/auditChannels.py`). Bands:

- **`[ALIVE]`** — ≥20% historical success rate over ≥50 requests
- **`[FLAKY]`** — 5–20% success
- **`[NEW]`** — <50 total requests, untested
- **`[DEAD]`** — <5% success over ≥500 requests, auto-disabled
- **`[ZOMBIE]`** — 0% success over ≥5000 requests, auto-disabled

The first key on each channel also carries a structured remark:

```
quality=ALIVE success=68093 failed=195917 rate=25.8% audited=2026-04-28
```

Re-run the audit any time to refresh tags:

```sh
OCTOPUS_BASE_URL=... OCTOPUS_USERNAME=... OCTOPUS_PASSWORD=... \
OCTOPUS_APPLY=1 python3 scripts/auditChannels.py
```

The audit is idempotent (no-op if everything is already current) and never
deletes channels — destructive cleanup is left to the operator.

## Generation taxonomy (5 chat-style groups)

| Group | Mode | Items | Top-priority members |
|---|---|---|---|
| `chat` | Failover | ~50 | `claude-sonnet-4.x`, `gpt-5.x`, `gpt-4.1`, `deepseek-v3.x`, `glm-5/4.7`, `qwen-plus`, `minimax-m2` |
| `chat-flagship` | Failover | ~55 | `claude-opus-4.x`, `gpt-5-pro`, `deepseek-v3.2-terminus`, `glm-5`, `qwen3-235b` |
| `chat-fast` | RoundRobin | ~10 | `gpt-5.x-mini`, `gpt-5-nano`, `gpt-4o-mini`, `claude-haiku-4.x`, `glm-air/flash`, `qwen-turbo`, `longcat-flash` |
| `code` | Failover | ~55 | `claude-opus-4.x`, `claude-sonnet-4.x`, `gpt-5-codex` family, `deepseek-v3` non-thinking, `qwen3-coder`, `codestral`, `doubao-seed-code` |
| `reason` | Failover | ~35 | `o1/o3/o4` family, `gpt-5-thinking`, `claude-opus-thinking`, `deepseek-r1`, `glm-thinking`, `qwen3-thinking`, `qwq-32b`, `kimi-k2` |

## Multimodal taxonomy (4 groups)

| Group | Mode | Items | Top members |
|---|---|---|---|
| `vision` | Failover | ~55 | `claude-opus-4.x` (native vision), `gpt-5.x`, `gpt-4o`, `glm-v` family, `qwen3-vl-instruct`, `gemini-flash/pro` |
| `vision-thinking` | Failover | small | `glm-v-thinking`, `qwen3-vl-thinking` |
| `image-gen` | Failover | small | `FLUX.1` (dev/pro/schnell), `Kolors`, `Qwen-Image` (incl. Edit), `gpt-image-1` |
| `video-gen` | Failover | small | `veo3` family, `Wan2.x` I2V/T2V, `sora` variants |

## Audio (1 group)

| Group | Mode | Items | Top members |
|---|---|---|---|
| `audio` | Failover | ~15 | `whisper`, `gpt-4o-audio/transcribe/realtime`, `tts-1`, `qwen3-tts/asr`, `ElevenLabs`, `Polly`, `fish-speech`, `CosyVoice`, `SenseVoice` |

## Embeddings & rerank (5 groups)

| Group | Mode | Items | Notes |
|---|---|---|---|
| `Embeddings-DB-MULTI-BGE-M3` | Failover | 6 | DB-bound, BGE-M3 1024d. **Default.** |
| `Embeddings-DB-EN-BGE-Large` | Failover | 2 | DB-bound. Original ch30 ZOMBIE; ch7 ALIVE fallback. |
| `Embeddings-DB-ZH-BGE-Large` | Failover | 2 | DB-bound. Original ch30 ZOMBIE; ch7 ALIVE fallback. |
| `embed-adhoc` | RoundRobin | ~10 | Mixed family. **Not safe for stored vectors.** |
| `rerank` | RoundRobin | ~5 | Qwen3-Reranker, BCE-Reranker, BGE-Reranker-v2-m3 |

### `rerank` route

`/v1/rerank` is live. Request shape (Cohere/Jina/Voyage compatible):

```sh
curl https://llmapi.iamwillywang.com/v1/rerank \
  -H "Authorization: Bearer sk-octopus-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "rerank",
    "query": "what is the capital of france",
    "documents": ["paris", "tokyo", "marseille"],
    "top_n": 2,
    "return_documents": true
  }'
```

Backed by `OutboundTypeOpenAIRerank` channels. The relay's compatibility
check (`outbound.IsRerankChannelType`) routes only to channels typed for
rerank, so adding a chat channel to the `rerank` group has no effect.

## How `model` is resolved on an inbound request

Octopus does **exact-name group lookup**, not regex routing. When a request
arrives at one of the inbound paths (`/v1/chat/completions`, `/v1/responses`,
`/v1/messages`, `/v1/embeddings`, `/v1/rerank`), the relay reads the `model`
field and looks it up directly in the in-memory group map keyed by group
name (`internal/op/group.go`, `GroupGetEnabledMap`).

That means:

- `{"model": "code"}` works — routes to the `code` group.
- `{"model": "claude-opus-4-6"}` does **not** work as a shortcut even though
  that model is one of the items inside the `code` group.
- Old names like `Deep-Reasoning` or `Agentic-Coder` do **not** work
  anymore — those groups were deleted during the redesign.

`match_regex` on a group does not affect inbound routing. It is used only by
the auto-group population logic in `internal/helper/channel.go`: when a
channel auto-syncs and reports its upstream model list, models matching a
group's regex get auto-added as items to that group.

### Calling Octopus vs. calling an upstream channel directly

These are two different things and use different keys:

```sh
# Through Octopus — uses your sk-octopus-... key. model = group name.
curl https://llmapi.iamwillywang.com/v1/chat/completions \
  -H "Authorization: Bearer sk-octopus-..." \
  -d '{"model":"code","messages":[{"role":"user","content":"hi"}]}'

# Direct to upstream — uses the upstream provider's key. model = upstream model.
# Group names like "code" are meaningless here.
curl https://upstream.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-upstream-..." \
  -d '{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hi"}]}'
```

## Operator workflow

```sh
# Re-converge groups (idempotent — diffs and applies only deltas)
OCTOPUS_BASE_URL=https://llmapi.iamwillywang.com \
OCTOPUS_USERNAME=<admin> OCTOPUS_PASSWORD=<pass> \
OCTOPUS_APPLY=1 \
python3 scripts/redesignGroups.py

# Re-audit channels (re-tag, re-disable based on latest stats)
OCTOPUS_APPLY=1 python3 scripts/auditChannels.py

# Dry-run (preview only): omit OCTOPUS_APPLY
```

### When to re-run

| When | Run |
|---|---|
| Channel stats change significantly | `auditChannels.py` then `redesignGroups.py` |
| New channel added with auto-sync | `redesignGroups.py` (picks up new models) |
| Want to change taxonomy / regexes | edit `TARGET_GROUPS`, then `redesignGroups.py` |
| Suspect dead channels are eating attempts | `auditChannels.py` (will tag/disable them) |

## Smoke tests

```sh
KEY=<your sk-octopus-... API key>
BASE=https://llmapi.iamwillywang.com
H_KEY="Authorization: Bearer $KEY"
H_CT="Content-Type: application/json"

# Chat-style groups
for g in chat chat-fast chat-flagship code reason vision; do
  echo "== $g =="
  curl -s --max-time 60 -H "$H_KEY" -H "$H_CT" "$BASE/v1/chat/completions" \
    -d "{\"model\":\"$g\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"max_tokens\":4}" \
    | python3 -c "import json,sys;d=json.load(sys.stdin);print('model =', d.get('model'))"
done

# Embeddings — verify the dimension matches your vector DB
for g in Embeddings-DB-MULTI-BGE-M3 embed-adhoc; do
  echo "== $g =="
  curl -s --max-time 60 -H "$H_KEY" -H "$H_CT" "$BASE/v1/embeddings" \
    -d "{\"model\":\"$g\",\"input\":\"hello\"}" \
    | python3 -c "import json,sys;d=json.load(sys.stdin);print('model =',d.get('model'),' dim =',len(d['data'][0]['embedding']))"
done
```

## Changelog

**2026-04-28 — full revamp**

- Replaced 17 ad-hoc groups (mixed casing, vague names, fragmented
  embeddings) with 15 capability-first groups + 3 DB-bound preserved.
- Audited 44 channels: 8 ALIVE, 9 FLAKY, 3 NEW, 14 DEAD (auto-disabled),
  10 ZOMBIE (auto-disabled).
- Group items now exclude DEAD/ZOMBIE/disabled channels and apply quality
  offsets to priorities — best-quality channels are tried first.
- Removed 923 stale group items pointing at disabled channels.
- Code patches: HTTP timeouts (dial/TLS/header read), per-attempt context
  cancellation for non-stream requests, weighted balancer correctness fix,
  cache shard-count power-of-2, JWT alg validation, atomic last-sync time,
  defensive nil-checks. See `.cosine/patches/` for details.
