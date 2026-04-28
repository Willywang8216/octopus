# Octopus Group Taxonomy

This is the live taxonomy after the 2026-04-28 redesign. Source of truth is
`scripts/redesignGroups.py`; this doc is the human-readable reference.

## Quick reference: what to call

| If you want… | Use group |
|---|---|
| General chat / Q&A | `chat` |
| Best possible quality, money no object | `chat-flagship` |
| Bulk processing, classification, extraction, summarization | `chat-fast` |
| Code generation, agentic coding (Cline, Aider, Cursor) | `code` |
| Math, planning, deep reasoning | `reason` |
| Image understanding (OCR, visual QA, document analysis) | `vision` |
| Visual reasoning (diagrams, math from photos) | `vision-thinking` |
| Text-to-image, image editing | `image-gen` |
| Text-to-video, image-to-video | `video-gen` |
| Speech-to-text (ASR) and text-to-speech (TTS) | `audio` |
| Reranking RAG retrieval results | `rerank` *(see caveat below)* |
| Ad-hoc embedding (one-off, NOT for stored vectors) | `embed-adhoc` |
| **Embedding text for / querying your vector DB** | one of `Embeddings-DB-*` |

## Embeddings — the vector-DB rule

**Once data is embedded into a vector database with model X, every future
query against that data must use exactly the same model X.** Different
embedding models produce vectors in different spaces with different
dimensionality and geometry — similarity scores between them are meaningless.

The DB-bound groups encode this contract: each one routes to exactly one
embedding model, so any caller using the group name gets vectors in the
correct space.

| Group | Model | Dimension | Use when |
|---|---|---|---|
| `Embeddings-DB-EN-BGE-Large` | `BAAI/bge-large-en-v1.5` | 1024 | English-only corpus |
| `Embeddings-DB-ZH-BGE-Large` | `BAAI/bge-large-zh-v1.5` | 1024 | Chinese-only corpus |
| `Embeddings-DB-MULTI-BGE-M3` | `BAAI/bge-m3` | 1024 (dense) | Mixed/multilingual, **default for new RAG** |

**Recommended default for new vector-DB projects: `Embeddings-DB-MULTI-BGE-M3`.**
BGE-M3 is the most capable BGE — supports 100+ languages, 8192-token context
(vs. 512 for BGE-Large), dense + sparse + ColBERT retrieval modes, all in
1024 dimensions. Future-proof for whatever data you throw at it.

**Never use `embed-adhoc` for stored vectors.** It round-robins across
multiple embedding model families with different dimensions (Qwen3 is 4096,
BCE is 768, NVIDIA NV-Embed is 4096, BGE is 1024). Two queries of the same
text could land on different models and produce incompatible vectors. It's
fine for one-off ad-hoc work or A/B testing only.

### Known issue: BGE-Large EN/ZH groups currently 503

Channels backing these two groups (channel 30, "21zys-embedding-split") are
currently disabled in your account (last 19,185 requests failed before it was
turned off). The groups exist with the correct model contract, but until you
add a working channel that serves `BAAI/bge-large-en-v1.5` and
`BAAI/bge-large-zh-v1.5`, calls return `503 no available channel`.
`Embeddings-DB-MULTI-BGE-M3` works fine because it has 5 channels.

## Full taxonomy (15 groups)

### Generation (5)

| Group | Mode | Items | Top picks |
|---|---|---|---|
| `chat` | Failover | 247 | claude-sonnet-4.x, gpt-5.x, gpt-4.1, deepseek-v3.x, glm-5, qwen-plus, minimax-m2 |
| `chat-flagship` | Failover | 195 | claude-opus-4.x, gpt-5-pro, deepseek-v3.2-terminus, glm-5, qwen3-235b |
| `chat-fast` | RoundRobin | 59 | gpt-5.x-mini, gpt-5-nano, gpt-4o-mini, claude-haiku-4.x, glm-air, qwen-turbo, longcat-flash |
| `code` | Failover | 256 | claude-opus-4.x, claude-sonnet-4.x, gpt-5-codex variants, deepseek-v3, qwen3-coder, codestral, doubao-seed-code |
| `reason` | Failover | 165 | o1/o3/o4 family, gpt-5-thinking, claude-opus-thinking, deepseek-r1, glm-thinking, qwen3-thinking, qwq-32b, kimi-k2 |

### Multimodal (4)

| Group | Mode | Items | Top picks |
|---|---|---|---|
| `vision` | Failover | 169 | claude-opus-4.x (native vision), gpt-5.x, gpt-4o, glm-v family, qwen3-vl-instruct, gemini-flash/pro |
| `vision-thinking` | Failover | 11 | glm-v-thinking, qwen3-vl-thinking |
| `image-gen` | Failover | 8 | FLUX.1 (dev/pro/schnell), Kolors, Qwen-Image (incl. Edit), gpt-image-1 |
| `video-gen` | Failover | 19 | veo3 family, Wan2.x I2V/T2V, sora variants |

### Audio (1)

| Group | Mode | Items | Top picks |
|---|---|---|---|
| `audio` | Failover | 57 | whisper, gpt-4o-audio/transcribe/realtime, tts-1, qwen3-tts/asr, ElevenLabs, Polly, fish-speech, CosyVoice, SenseVoice |

### Embeddings & rerank (5)

| Group | Mode | Items | Notes |
|---|---|---|---|
| `Embeddings-DB-MULTI-BGE-M3` | Failover | 5 | DB-bound, BGE-M3 1024d. **Default for vector-DB.** |
| `Embeddings-DB-EN-BGE-Large` | Failover | 1 | DB-bound. ⚠ Channel currently disabled. |
| `Embeddings-DB-ZH-BGE-Large` | Failover | 1 | DB-bound. ⚠ Channel currently disabled. |
| `embed-adhoc` | RoundRobin | 32 | Mixed-family pool. **Not safe for stored vectors.** |
| `rerank` | RoundRobin | 21 | Qwen3-Reranker, BCE-Reranker, BGE-Reranker-v2-m3. **See caveat below.** |

### `rerank` caveat

Octopus only exposes 4 inbound paths: `/v1/chat/completions`, `/v1/responses`,
`/v1/messages`, `/v1/embeddings`. There is **no `/v1/rerank` route** in this
gateway. The `rerank` group exists so that:

1. Reranker model names show up in `/v1/models` for client discovery.
2. If/when an operator adds a rerank route to the relay handler, the routing
   pool is already configured.

Today, to actually invoke a reranker, you must call its underlying provider
directly (e.g. SiliconFlow's `/v1/rerank`) bypassing this gateway, or add the
rerank inbound type to `internal/server/handlers/relay.go` and a corresponding
transformer.

## Match-regex behaviour

Every group has a `match_regex` like `(?i)^chat$` so callers can request the
group by its canonical name as the `model` field. The original system also
let model regexes route to groups; the new design intentionally tightens this
to exact group-name matches. This is a behaviour change.

If you want a model name like `claude-opus-4-6` to also route into the `code`
group, edit `match_regex` for `code` in `redesignGroups.py` and re-run the
script — it'll diff and update.

## Routing modes

| Mode | Used for | Behaviour |
|---|---|---|
| `Failover` (3) | Most groups | Try priority 1 channel/model first; on failure, fall through to higher priority numbers. |
| `RoundRobin` (1) | `chat-fast`, `embed-adhoc`, `rerank` | Cycle through items at the same priority for even load. |

## Operator workflow

```sh
# Authoritative re-converge (idempotent — diffs and applies only deltas)
OCTOPUS_BASE_URL=https://llmapi.iamwillywang.com \
OCTOPUS_USERNAME=<admin> OCTOPUS_PASSWORD=<pass> \
OCTOPUS_APPLY=1 \
python3 scripts/redesignGroups.py

# Dry-run (preview only, no writes)
OCTOPUS_BASE_URL=... OCTOPUS_USERNAME=... OCTOPUS_PASSWORD=... \
python3 scripts/redesignGroups.py
```

To change membership rules, edit `TARGET_GROUPS` in `scripts/redesignGroups.py`
and re-run with `OCTOPUS_APPLY=1`. The script will:

- create any missing groups
- add new items, update changed items, delete removed items
- never touch the `Embeddings-DB-*` groups (preserved verbatim)
- never delete groups outside the `RETIRE_GROUPS` allow-list

## Smoke tests

```sh
KEY=<your sk-octopus-... API key>
BASE=https://llmapi.iamwillywang.com
H="-H 'Authorization: Bearer $KEY' -H 'Content-Type: application/json'"

# Chat-style groups
for g in chat chat-fast chat-flagship code reason vision vision-thinking; do
  echo "== $g =="
  curl -s $H "$BASE/v1/chat/completions" \
    -d "{\"model\":\"$g\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"max_tokens\":4}" \
    | python3 -c "import json,sys;d=json.load(sys.stdin);print('model =', d.get('model'))"
done

# Embeddings — verify dimensions match what your vector DB expects
for g in Embeddings-DB-MULTI-BGE-M3 embed-adhoc; do
  echo "== $g =="
  curl -s $H "$BASE/v1/embeddings" \
    -d "{\"model\":\"$g\",\"input\":\"hello\"}" \
    | python3 -c "import json,sys;d=json.load(sys.stdin);print('model =',d.get('model'),' dim =',len(d['data'][0]['embedding']))"
done
```
