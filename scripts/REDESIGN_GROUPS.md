# Octopus Group Taxonomy — Redesign Reference

This document is the single source of truth for the new group layout. The
migration script `scripts/redesignGroups.py` converges the live system to
match it.

## How to run the migration

```sh
# Dry-run (default): prints diff, makes no changes.
OCTOPUS_BASE_URL=https://llmapi.iamwillywang.com \
OCTOPUS_USERNAME=<admin-user> \
OCTOPUS_PASSWORD=<admin-pass> \
python3 scripts/redesignGroups.py

# Apply: creates/updates the new groups. Old groups are NOT deleted yet.
OCTOPUS_APPLY=1 OCTOPUS_BASE_URL=... OCTOPUS_USERNAME=... OCTOPUS_PASSWORD=... \
python3 scripts/redesignGroups.py

# Final cutover: delete the retired old groups.
OCTOPUS_APPLY=1 OCTOPUS_DELETE_OLD=1 OCTOPUS_BASE_URL=... \
OCTOPUS_USERNAME=... OCTOPUS_PASSWORD=... \
python3 scripts/redesignGroups.py
```

The script is idempotent: it diffs current state against the target taxonomy
defined in `TARGET_GROUPS` and only sends the deltas. Re-running is a no-op
once everything is converged.

## Recommended rollout

1. **Dry-run** — review the diff, sanity-check it matches your channel pool.
2. **Apply (no delete)** — new groups appear alongside old. Test downstream.
3. **Notify clients** — anyone using old names like `Agentic-Coder` should
   migrate to the new capability-first names.
4. **Apply with delete** — once you've confirmed nothing breaks, retire the
   old groups.

## Target taxonomy (15 groups, plus 3 DB-bound preserved verbatim)

### Generation / chat

| Name | Mode | Purpose | Preferred members (priority asc) |
|---|---|---|---|
| `chat` | Failover | Default chat/instruct, balanced quality+latency | qwen3-30b-a3b-instruct-2507 → qwen3-next-80b-a3b-instruct → qwen3.5-35b-a3b → qwen3.6-35b-a3b → qwen3-32b → qwen3.5-27b |
| `chat-flagship` | Failover | Hardest tasks, highest quality | qwen3-235b-a22b-instruct-2507 → qwen3.5-397b-a17b → qwen3.5-122b-a10b → qwen3-next-80b-a3b-instruct |
| `chat-fast` | RoundRobin | Throughput / low-latency cheap tier | qwen3-8b, qwen3-14b, qwen3.5-9b (round-robin) → qwen3-30b-a3b-instruct-2507 |
| `code` | Failover | Coding & agentic coding | qwen3-coder-480b-a35b-instruct → qwen3-coder-30b-a3b-instruct → qwen2.5-coder-32b-instruct |
| `reason` | Failover | Explicit chain-of-thought / thinking-mode | qwen3-235b-a22b-thinking-2507 → qwen3-next-80b-a3b-thinking → qwen3-30b-a3b-thinking-2507 → qwq-32b |

### Multimodal

| Name | Mode | Purpose | Preferred members |
|---|---|---|---|
| `vision` | Failover | Image+text VL (no audio) | qwen3-vl-235b-a22b-instruct → qwen3-vl-32b-instruct → qwen3-vl-30b-a3b-instruct → qwen2.5-vl-72b-instruct → qwen3-vl-8b-instruct |
| `vision-thinking` | Failover | VL with explicit reasoning | qwen3-vl-32b-thinking → qwen3-vl-30b-a3b-thinking → qwen3-vl-8b-thinking |
| `omni` | Failover | Audio + vision + text unified | qwen3-omni-30b-a3b-instruct → qwen3-omni-30b-a3b-thinking → qwen3-omni-30b-a3b-captioner |
| `audio` | Failover | ASR / TTS / speech (falls back to omni) | dedicated audio channels first; qwen3-omni fallback at priority 90 |

### Embeddings

DB-bound groups — names preserved verbatim because they back real vector
indexes. Items are NOT modified by the script.

| Name | Mode | Purpose |
|---|---|---|
| `Embeddings-DB-ZH-BGE-Large` | RoundRobin | ZH BGE-Large vector index |
| `Embeddings-DB-EN-BGE-Large` | RoundRobin | EN BGE-Large vector index |
| `Embeddings-DB-MULTI-BGE-M3` | RoundRobin | Multilingual BGE-M3 vector index |

Family pools — for new ad-hoc work:

| Name | Mode | Purpose | Members |
|---|---|---|---|
| `embed-qwen3` | RoundRobin | Qwen3 embeddings | qwen3-embedding-0.6b/4b/8b → qwen3-vl-embedding-8b |
| `embed-bge` | RoundRobin | BGE embeddings (non-DB) | any non-DB BGE channels |

### Rerankers

| Name | Mode | Purpose | Members |
|---|---|---|---|
| `rerank` | RoundRobin | RAG reranker pool | qwen3-reranker-0.6b/4b/8b → BCE/BGE rerankers |

### Legacy

| Name | Mode | Purpose |
|---|---|---|
| `legacy` | Failover | Quarantine for qwen1.5-* and pre-2507 originals. Backwards compat only. |

## Old → New mapping

| Old group | Action | New group |
|---|---|---|
| Agentic-Coder | rename + repopulate | `code` |
| Audio-Speech-Group | rename | `audio` |
| Deep-Reasoning | merge | `reason` |
| reasoning | merge (drops duplicate) | `reason` |
| Embeddings-BGE | rename | `embed-bge` |
| Embeddings-Qwen3 | rename | `embed-qwen3` |
| Embeddings-Experiment-Universal | drop | — |
| Embeddings-BCE | drop or absorb into `rerank`/`embed-bge` | — |
| Embeddings-NVIDIA | drop | — |
| Embeddings-DB-MULTI-BGE-M3 | preserve verbatim | unchanged |
| Embeddings-DB-ZH-BGE-Large | preserve verbatim | unchanged |
| Embeddings-DB-EN-BGE-Large | preserve verbatim | unchanged |
| Flash-Efficiency | rename + repopulate | `chat-fast` |
| Multimodal-Generation-Groups | split | `vision` + `omni` |
| Omni-Intelligence | rename | `omni` |
| Rerankers-Qwen3 | rename | `rerank` |
| The-MoE-Safety-Net | rename | `chat-flagship` |

## Match-regex behaviour

Each new group has a `match_regex` so callers can hit it either by:

- the canonical group name (e.g. `model: "code"`), or
- any underlying model name that the regex matches (e.g. `model: "qwen3-coder-480b-a35b-instruct"` also routes to `code`).

This means migration of clients is gradual — they can stop hardcoding model
names and switch to capability names at their own pace.

## Smoke-test commands

```sh
KEY=sk-octopus-DUYIPoWjQChAtbj3dUSxeVmO6fpELGGoxP4aZxhqwEnWo3Gp
BASE=https://llmapi.iamwillywang.com

# Chat groups
for g in chat chat-fast chat-flagship code reason; do
  echo "== $g =="
  curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    "$BASE/v1/chat/completions" \
    -d "{\"model\":\"$g\",\"messages\":[{\"role\":\"user\",\"content\":\"ping\"}],\"max_tokens\":4}" \
    | python3 -m json.tool | head -20
done

# Vision (text-only smoke test, no image)
curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  "$BASE/v1/chat/completions" \
  -d '{"model":"vision","messages":[{"role":"user","content":"hello"}],"max_tokens":4}'

# Embeddings
for g in embed-qwen3 embed-bge Embeddings-DB-MULTI-BGE-M3; do
  echo "== $g =="
  curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    "$BASE/v1/embeddings" \
    -d "{\"model\":\"$g\",\"input\":\"hello\"}" \
    | python3 -c "import json,sys; d=json.load(sys.stdin); print('dim =', len(d['data'][0]['embedding']))"
done

# Rerank (path depends on transformer; usually /v1/rerank)
curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  "$BASE/v1/rerank" \
  -d '{"model":"rerank","query":"hello","documents":["world","goodbye"]}'
```
