#!/usr/bin/env python3
"""
Octopus group taxonomy redesign — idempotent migration.

Reads admin credentials from env, mints a JWT, inventories existing channels
and groups, then converges live state to the target taxonomy defined in this
file. Safe to re-run.

Env:
  OCTOPUS_BASE_URL      e.g. https://llmapi.iamwillywang.com
  OCTOPUS_USERNAME      admin username
  OCTOPUS_PASSWORD      admin password
  OCTOPUS_APPLY         set to "1" to apply changes; otherwise dry-run only
  OCTOPUS_DELETE_OLD    set to "1" to delete old groups not in the target set
                        (only after you've verified the new ones work)

Usage:
  OCTOPUS_BASE_URL=https://llmapi.iamwillywang.com \
  OCTOPUS_USERNAME=... OCTOPUS_PASSWORD=... \
  python scripts/redesignGroups.py            # dry-run

  OCTOPUS_APPLY=1 python scripts/redesignGroups.py    # apply
"""

import json
import os
import re
import sys
import urllib.error
import urllib.request
from typing import Any

# Group modes mirror internal/model/group.go
MODE_ROUND_ROBIN = 1
MODE_RANDOM = 2
MODE_FAILOVER = 3
MODE_WEIGHTED = 4

DEFAULT_FIRST_TOKEN_TIMEOUT = 30  # seconds
DEFAULT_SESSION_KEEP_TIME = 0     # disabled

# Target taxonomy. `members` is a list of regexes matched against model names
# discovered from the live channel inventory. Every channel exposing a matching
# model is added as a GroupItem with the given priority (lower = preferred).
#
# `match_regex` is the inbound model-name regex that callers can hit to route
# into this group (in addition to calling the group by its literal name).
# Patterns are evaluated against the live (channel_id, model_name) inventory
# discovered from existing groups + auto-sync. Lower priority = preferred.
# Designed for the real multi-provider catalog: OpenAI/Anthropic/GLM/DeepSeek/
# MiniMax/Qwen/Kimi/Doubao/Llama/Mistral/Grok/Gemini and friends.

# Exclusion guard for chat-style groups so they don't accidentally pull in
# code/vision/audio/embed/rerank/image-gen models.
NOT_SPECIALIST = (
    r"(?!.*(coder|-code|codestral|-vl-|qwen.*vl|llava|internvl|"
    r"glm-[45]\.\d?v|glm-[45]v|glm-5v|"
    r"omni|whisper|tts-1|gpt-(?:4o-mini-)?(?:tts|audio|realtime|transcribe)|"
    r"asr|speech|sovits|cosyvoice|sensevoice|elevenlabs|polly|fish-speech|"
    r"moss-tts|indextts|telespeech|"
    r"flux|kolors|wan2|gpt-image|veo3|imagen|stable-diffusion|sdxl|"
    r"midjourney|hidream|qwen-image|"
    r"embed|rerank))"
)

TARGET_GROUPS: list[dict[str, Any]] = [
    # ── Generation ────────────────────────────────────────────────────────
    {
        "name": "chat",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^chat$",
        "purpose": "Default chat/instruct, balanced quality+latency. Mid-tier.",
        "members": [
            # Strong mid-tier across providers, no thinking, no specialists
            (rf"(?i)^{NOT_SPECIALIST}(?:claude-sonnet-4(?:[.-]\d)*|"
             r"claude-haiku-4(?:[.-]\d)*|"
             r"anthropic-sonnet-4|anthropic-haiku-4)(?:-\d{8})?$", 10),
            (rf"(?i)^{NOT_SPECIALIST}gpt-5(?:[.-]\d+)?(?:-mini)?$", 15),
            (rf"(?i)^{NOT_SPECIALIST}gpt-4\.1(?:-mini)?$", 20),
            (rf"(?i)^{NOT_SPECIALIST}gpt-4o(?:-mini)?$", 25),
            (rf"(?i)^{NOT_SPECIALIST}deepseek-v3(?:[.-]\d+)?(?:-fast)?$", 30),
            (rf"(?i)^{NOT_SPECIALIST}glm-(?:4\.[567]|5)(?:-air|-flash)?$", 35),
            (rf"(?i)^{NOT_SPECIALIST}qwen-(?:plus|max|turbo)-latest$", 40),
            (rf"(?i)^{NOT_SPECIALIST}minimax-m2(?:\.\d+)?$", 45),
        ],
    },
    {
        "name": "chat-flagship",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^chat-flagship$",
        "purpose": "Hardest tasks, highest quality. Top-tier from each family.",
        "members": [
            (rf"(?i)^{NOT_SPECIALIST}claude-opus-4(?:[.-]\d+)*(?:-\d{{8}})?$", 10),
            (rf"(?i)^{NOT_SPECIALIST}anthropic[-:/]claude-opus-4(?:[.-]\d+)*$", 12),
            (rf"(?i)^{NOT_SPECIALIST}gpt-5(?:[.-]\d+)?(?:-pro|-thinking)?$", 15),
            (rf"(?i)^{NOT_SPECIALIST}deepseek-v3\.[12](?:-terminus)?$", 20),
            (rf"(?i)^{NOT_SPECIALIST}glm-(?:5(?:\.\d)?|4\.[67])(?:-deepsearch)?$", 25),
            (rf"(?i)^{NOT_SPECIALIST}qwen3-235b-a22b(?:-2507)?(?:-instruct)?$", 30),
            (rf"(?i)^{NOT_SPECIALIST}meta/llama-3\.1-405b.*$", 35),
            (rf"(?i)^{NOT_SPECIALIST}mistralai/mistral-large.*$", 40),
        ],
    },
    {
        "name": "chat-fast",
        "mode": MODE_ROUND_ROBIN,
        "match_regex": r"(?i)^chat-fast$",
        "purpose": "High-throughput, low-latency. Use for bulk classification, extraction, summarization.",
        "members": [
            (rf"(?i)^{NOT_SPECIALIST}gpt-5\.\d+-mini$", 10),
            (rf"(?i)^{NOT_SPECIALIST}gpt-5-nano$", 10),
            (rf"(?i)^{NOT_SPECIALIST}gpt-4o-mini$", 10),
            (rf"(?i)^{NOT_SPECIALIST}claude-haiku-4(?:[.-]\d+)*$", 10),
            (rf"(?i)^{NOT_SPECIALIST}glm-(?:4\.[567]|5)-(?:air|flash)$", 10),
            (rf"(?i)^{NOT_SPECIALIST}qwen-turbo-latest$", 10),
            (rf"(?i)^{NOT_SPECIALIST}deepseek-v3(?:[.-]\d+)?-fast$", 10),
            (rf"(?i)^{NOT_SPECIALIST}longcat-flash(?!-thinking)$", 15),
            (rf"(?i)^{NOT_SPECIALIST}cerebras[-/]gpt-oss-120b$", 15),
            (rf"(?i)^{NOT_SPECIALIST}gpt-oss-120b$", 15),
        ],
    },
    {
        "name": "code",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^code$",
        "purpose": "Coding and agentic coding (Cline, Aider, Cursor).",
        "members": [
            (r"(?i)^claude-opus-4(?:[.-]\d+)*(?:-\d{8})?(?!-thinking)$", 10),
            (r"(?i)^anthropic[-:/]claude-opus-4(?:[.-]\d+)*$", 11),
            (r"(?i)^claude-sonnet-4(?:[.-]\d+)*(?:-\d{8})?(?!-thinking)$", 15),
            (r"(?i)^gpt-5(?:\.\d+)?-codex(?:-(?:high|medium|low|mini|fast|auto))*$", 20),
            (r"(?i)^gpt-5\.\d+-codex$", 20),
            (r"(?i)^deepseek-v3(?:[.-]\d+)?(?:-0324|-1|-1-250821)?(?!-thinking)$", 25),
            (r"(?i)^qwen3-coder(?:-flash|-plus)?(?:-\d{4}-\d{2}-\d{2})?$", 30),
            (r"(?i)^alibaba/qwen3-coder-(?:flash|plus)-\d{4}-\d{2}-\d{2}$", 30),
            (r"(?i)^Qwen/Qwen3-Coder-.*-Instruct$", 30),
            (r"(?i)^codestral.*$", 35),
            (r"(?i)^doubao-seed-code-.*$", 40),
            (r"(?i)^cerebras[-/]?gpt-oss-120b$", 45),
        ],
    },
    {
        "name": "reason",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^reason$",
        "purpose": "Explicit chain-of-thought / extended thinking. Use for math, planning, deep analysis.",
        "members": [
            (r"(?i)^o[134](?:-(?:mini|preview|pro))?(?:-\d{4}-\d{2}-\d{2})?$", 10),
            (r"(?i)^gpt-5-(?:thinking|reasoning|pro)$", 12),
            (r"(?i)^claude-opus-4(?:[.-]\d+)*-thinking$", 15),
            (r"(?i)^anthropic[-:/]claude-opus-4(?:[.-]\d+)*[-:]thinking$", 16),
            (r"(?i)^claude-sonnet-4(?:[.-]\d+)*-thinking$", 20),
            (r"(?i)^claude-3[.-]7-sonnet(?:-\d{8})?-thinking$", 22),
            (r"(?i)^deepseek-r1(?:-\d+)?(?:-distill.*)?$", 25),
            (r"(?i)^deepseek-v3(?:[.-]\d+)?-thinking$", 27),
            (r"(?i)^glm-(?:[45](?:\.\d)?|5(?:\.\d)?-turbo)-(?:.*-)?thinking(?:-search)?$", 30),
            (r"(?i)^qwen3-235b-a22b(?:-2507)?-thinking$", 35),
            (r"(?i)^Qwen/Qwen3-Next-80B-A3B-Thinking$", 36),
            (r"(?i)^qwq-32b$", 40),
            (r"(?i)^longcat-flash-thinking(?:-\d+)?$", 42),
            (r"(?i)^kimi-k2(?:-instruct)?$", 45),
            (r"(?i)^moonshotai/kimi-k2-instruct.*$", 45),
        ],
    },
    # ── Multimodal ────────────────────────────────────────────────────────
    {
        "name": "vision",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^vision$",
        "purpose": "Image understanding (no thinking). Use for OCR, visual QA, document analysis.",
        "members": [
            (r"(?i)^claude-opus-4(?:[.-]\d+)*(?!-thinking)$", 10),
            (r"(?i)^claude-sonnet-4(?:[.-]\d+)*(?!-thinking)$", 15),
            (r"(?i)^gpt-5(?:[.-]\d+)?(?:-mini)?$", 20),
            (r"(?i)^gpt-4o(?:-mini)?$", 25),
            (r"(?i)^glm-(?:4\.[1-7]|5\.?\d?)v(?:-flash|-search)?(?!-thinking)$", 30),
            (r"(?i)^zai-glm-4\.\dv-flash$", 32),
            (r"(?i)^Qwen/Qwen3-VL-.*-Instruct$", 35),
            (r"(?i)^qwen3-vl-.*-instruct$", 35),
            (r"(?i)^Pro/Qwen/Qwen2\.5-VL-.*-Instruct$", 40),
            (r"(?i)^gemini-.*-(?:flash|pro)(?:-vision)?(?:-\d+)?$", 45),
        ],
    },
    {
        "name": "vision-thinking",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^vision-thinking$",
        "purpose": "Image + extended reasoning. Use for complex visual reasoning, math from photos, diagram analysis.",
        "members": [
            (r"(?i)^glm-(?:4\.[1-7]|5\.?\d?)v(?:-flash|-search)?-thinking(?:-search)?$", 10),
            (r"(?i)^GLM-4\.1V-Thinking-FlashX$", 12),
            (r"(?i)^GLM-5V-Turbo-thinking$", 12),
            (r"(?i)^qwen3-vl-.*-thinking$", 15),
            (r"(?i)^Qwen/Qwen3-VL-.*-Thinking$", 15),
        ],
    },
    {
        "name": "image-gen",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^image-gen$",
        "purpose": "Text-to-image and image editing.",
        "members": [
            (r"(?i)^(?:Pro/)?black-forest-labs/FLUX\.1-(?:dev|pro|schnell)$", 10),
            (r"(?i)^Kwai-Kolors/Kolors$", 15),
            (r"(?i)^Qwen/Qwen-Image(?:-Edit(?:-\d+)?)?$", 20),
            (r"(?i)^gpt-image(?:-1)?$", 25),
            (r"(?i)^hidream.*$", 30),
        ],
    },
    {
        "name": "video-gen",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^video-gen$",
        "purpose": "Text-to-video and image-to-video.",
        "members": [
            (r"(?i)^veo3(?:\.\d+)?(?:-(?:fast|pro|4k|frames|components))*(?:-\d+k)?$", 10),
            (r"(?i)^Wan-AI/Wan2\.\d+-(?:I2V|T2V).*$", 15),
            (r"(?i)^sora.*$", 20),
        ],
    },
    {
        "name": "audio",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^audio$",
        "purpose": "ASR (speech-to-text) and TTS (text-to-speech).",
        "members": [
            (r"(?i)^whisper(?:-1)?$", 10),
            (r"(?i)^gpt-4o(?:-mini)?-(?:tts|transcribe|audio-preview|realtime-preview)(?:-\d{4}-\d{2}-\d{2})?$", 12),
            (r"(?i)^gpt-(?:audio|realtime)-\d{4}-\d{2}-\d{2}$", 12),
            (r"(?i)^tts-1(?:-hd)?(?:-\d{4})?$", 15),
            (r"(?i)^qwen3-(?:tts-flash|asr)(?:-\d{4}-\d{2}-\d{2})?$", 20),
            (r"(?i)^elevenlabs(?:[-_].+)?$", 25),
            (r"(?i)^polly(?:[-_].+)?$", 30),
            (r"(?i)^openai-audio$", 12),
            (r"(?i)^fishaudio/fish-speech-.*$", 35),
            (r"(?i)^FunAudioLLM/(?:CosyVoice|SenseVoice).*$", 40),
            (r"(?i)^IndexTeam/IndexTTS-.*$", 45),
            (r"(?i)^fnlp/MOSS-TTSD-.*$", 45),
            (r"(?i)^RVC-Boss/GPT-SoVITS$", 50),
            (r"(?i)^TeleAI/TeleSpeechASR$", 50),
        ],
    },
    # ── RAG infrastructure ────────────────────────────────────────────────
    {
        "name": "rerank",
        "mode": MODE_ROUND_ROBIN,
        "match_regex": r"(?i)^rerank$",
        "purpose": "RAG reranker pool. Used after vector retrieval to re-score top-k.",
        "members": [
            (r"(?i)^Qwen/Qwen3-Reranker-8B$", 10),
            (r"(?i)^Qwen/Qwen3-Reranker-4B$", 10),
            (r"(?i)^Qwen/Qwen3-Reranker-0\.6B$", 15),
            # any future BCE/BGE rerankers picked up automatically:
            (r"(?i)^.*[/-]bce.*rerank.*$", 20),
            (r"(?i)^.*[/-]bge.*rerank.*$", 20),
        ],
    },
    {
        "name": "embed-adhoc",
        "mode": MODE_ROUND_ROBIN,
        "match_regex": r"(?i)^embed-adhoc$",
        "purpose": "Ad-hoc embedding pool. NOT for vector-DB use — different calls may hit different model families. Use the Embeddings-DB-* groups for indexed data.",
        "members": [
            (r"(?i)^Qwen/Qwen3-Embedding-(?:0\.6|4|8)B$", 10),
            (r"(?i)^netease-youdao/bce-embedding-base_v1$", 10),
            (r"(?i)^nvidia/(?:embed-qa-4|nv-embed.*|llama-3\.2-nemoretriever-.*embed.*)$", 15),
            (r"(?i)^snowflake/arctic-embed-l$", 15),
        ],
    },
]

# DB-bound groups: names preserved verbatim because they back real vector
# indexes. Members come from existing items rather than regex discovery — we
# don't infer; we copy what's already there.
DB_BOUND_GROUP_NAMES = {
    "Embeddings-DB-ZH-BGE-Large",
    "Embeddings-DB-EN-BGE-Large",
    "Embeddings-DB-MULTI-BGE-M3",
}

# Old groups to retire after the new ones are verified (driven by OCTOPUS_DELETE_OLD).
RETIRE_GROUPS = {
    "Agentic-Coder",
    "Audio-Speech-Group",
    "Deep-Reasoning",
    "reasoning",
    "Embeddings-BGE",
    "Embeddings-Qwen3",
    "Embeddings-Experiment-Universal",
    "Embeddings-BCE",
    "Embeddings-NVIDIA",
    "Flash-Efficiency",
    "Multimodal-Generation-Groups",
    "Omni-Intelligence",
    "Rerankers-Qwen3",
    "The-MoE-Safety-Net",
}


class Client:
    def __init__(self, base_url: str, token: str | None = None):
        self.base_url = base_url.rstrip("/")
        self.token = token

    def _request(self, method: str, path: str, body: dict | None = None) -> Any:
        url = f"{self.base_url}{path}"
        data = None
        headers = {"Content-Type": "application/json", "Accept": "application/json", "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"}
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"
        if body is not None:
            data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(url, data=data, method=method, headers=headers)
        try:
            with urllib.request.urlopen(req) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            body_text = e.read().decode("utf-8", errors="replace")
            raise SystemExit(f"HTTP {e.code} on {method} {path}: {body_text}")
        if isinstance(payload, dict) and payload.get("code") not in (None, 0, 200):
            raise SystemExit(f"API error on {method} {path}: {payload}")
        return payload.get("data") if isinstance(payload, dict) else payload

    def login(self, username: str, password: str) -> str:
        data = self._request("POST", "/api/v1/user/login",
                             {"username": username, "password": password})
        self.token = data["token"]
        return self.token

    def channel_list(self) -> list[dict]:
        return self._request("GET", "/api/v1/channel/list") or []

    def group_list(self) -> list[dict]:
        return self._request("GET", "/api/v1/group/list") or []

    def group_create(self, group: dict) -> dict:
        return self._request("POST", "/api/v1/group/create", group)

    def group_update(self, req: dict) -> dict:
        return self._request("POST", "/api/v1/group/update", req)

    def group_delete(self, group_id: int) -> Any:
        return self._request("DELETE", f"/api/v1/group/delete/{group_id}")


def build_model_index(channels: list[dict], groups: list[dict]) -> dict[str, list[int]]:
    """Map model_name -> [channel_id, ...] from the live inventory.

    Octopus channels don't expose a model list directly (auto_sync discovers
    models per channel at runtime). The reliable source of truth is the union
    of every existing group's items, which encode (channel_id, model_name)
    pairs that are known to work.

    Also accepts hints from channels' `models`/`custom_model` fields if any
    operator has filled them in, but doesn't depend on them."""
    index: dict[str, set[int]] = {}

    # Primary source: union of all group items.
    for g in groups:
        for it in g.get("items") or []:
            cid = it.get("channel_id")
            mn = it.get("model_name")
            if cid is None or not mn:
                continue
            index.setdefault(mn, set()).add(int(cid))

    # Secondary hint: explicit channel.model / channel.custom_model fields.
    for ch in channels:
        cid = ch.get("id") or ch.get("ID")
        if cid is None:
            continue
        for field in ("models", "model_list", "supported_models", "model", "custom_model"):
            v = ch.get(field)
            if not v:
                continue
            if isinstance(v, str):
                names = [m.strip() for m in re.split(r"[,\s]+", v) if m.strip()]
            elif isinstance(v, list):
                names = []
                for m in v:
                    if isinstance(m, str):
                        names.append(m)
                    elif isinstance(m, dict):
                        n = m.get("name") or m.get("model")
                        if n:
                            names.append(n)
            else:
                names = []
            for n in names:
                index.setdefault(n, set()).add(int(cid))

    return {k: sorted(v) for k, v in index.items()}


def discover_members(
    member_rules: list[tuple[str, int]],
    model_index: dict[str, list[int]],
) -> list[dict]:
    """Resolve regex-based member rules against the live model index.
    Returns a list of {channel_id, model_name, priority, weight=1} dicts,
    deduped on (channel_id, model_name)."""
    seen: set[tuple[int, str]] = set()
    items: list[dict] = []
    for pattern, priority in member_rules:
        rx = re.compile(pattern)
        for model_name, channel_ids in model_index.items():
            if not rx.search(model_name):
                continue
            for cid in channel_ids:
                key = (cid, model_name)
                if key in seen:
                    continue
                seen.add(key)
                items.append({
                    "channel_id": cid,
                    "model_name": model_name,
                    "priority": priority,
                    "weight": 1,
                })
    return items


def diff_items(current: list[dict], desired: list[dict]) -> tuple[list[dict], list[dict], list[int]]:
    """Compute (to_add, to_update, to_delete_ids) for an existing group.
    Match by (channel_id, model_name)."""
    cur_by_key = {(c["channel_id"], c["model_name"]): c for c in current}
    des_by_key = {(d["channel_id"], d["model_name"]): d for d in desired}

    to_add: list[dict] = []
    to_update: list[dict] = []
    to_delete: list[int] = []

    for key, d in des_by_key.items():
        if key not in cur_by_key:
            to_add.append(d)
        else:
            c = cur_by_key[key]
            if c.get("priority") != d.get("priority") or c.get("weight") != d.get("weight"):
                to_update.append({
                    "id": c["id"],
                    "priority": d["priority"],
                    "weight": d["weight"],
                })
    for key, c in cur_by_key.items():
        if key not in des_by_key:
            to_delete.append(c["id"])
    return to_add, to_update, to_delete


def converge_group(client: Client, target: dict, existing: dict | None,
                   model_index: dict[str, list[int]], apply: bool) -> None:
    desired_items = discover_members(target["members"], model_index)
    name = target["name"]

    if existing is None:
        print(f"  + create group {name!r} ({len(desired_items)} items)")
        if apply:
            created = client.group_create({
                "name": name,
                "mode": target["mode"],
                "match_regex": target.get("match_regex", ""),
                "first_token_time_out": DEFAULT_FIRST_TOKEN_TIMEOUT,
                "session_keep_time": DEFAULT_SESSION_KEEP_TIME,
            })
            gid = created["id"]
            if desired_items:
                client.group_update({
                    "id": gid,
                    "items_to_add": [
                        {k: v for k, v in i.items() if k != "id"} for i in desired_items
                    ],
                })
        return

    to_add, to_update, to_delete = diff_items(existing.get("items") or [], desired_items)
    needs_meta = (
        existing.get("mode") != target["mode"]
        or existing.get("match_regex") != target.get("match_regex", "")
    )
    if not (to_add or to_update or to_delete or needs_meta):
        print(f"  = group {name!r} already converged")
        return
    print(f"  ~ update group {name!r}: +{len(to_add)} items, "
          f"~{len(to_update)} items, -{len(to_delete)} items"
          f"{' (meta)' if needs_meta else ''}")
    if apply:
        req: dict[str, Any] = {"id": existing["id"]}
        if needs_meta:
            req["mode"] = target["mode"]
            req["match_regex"] = target.get("match_regex", "")
        if to_add:
            req["items_to_add"] = [
                {k: v for k, v in i.items() if k != "id"} for i in to_add
            ]
        if to_update:
            req["items_to_update"] = to_update
        if to_delete:
            req["items_to_delete"] = to_delete
        client.group_update(req)


def main() -> int:
    base_url = os.environ.get("OCTOPUS_BASE_URL")
    username = os.environ.get("OCTOPUS_USERNAME")
    password = os.environ.get("OCTOPUS_PASSWORD")
    apply = os.environ.get("OCTOPUS_APPLY") == "1"
    delete_old = os.environ.get("OCTOPUS_DELETE_OLD") == "1"

    if not (base_url and username and password):
        print("ERROR: set OCTOPUS_BASE_URL, OCTOPUS_USERNAME, OCTOPUS_PASSWORD",
              file=sys.stderr)
        return 2

    print(f"Mode: {'APPLY' if apply else 'DRY-RUN'} "
          f"(delete_old={'on' if delete_old else 'off'})")
    print(f"Target: {base_url}")

    client = Client(base_url)
    client.login(username, password)
    print("Authenticated.")

    channels = client.channel_list()
    groups = client.group_list()
    model_index = build_model_index(channels, groups)
    print(f"Discovered {len(channels)} channels, "
          f"{len(model_index)} unique models, "
          f"{len(groups)} existing groups.")

    by_name = {g["name"]: g for g in groups}

    print("\n== Converging target groups ==")
    for target in TARGET_GROUPS:
        converge_group(client, target, by_name.get(target["name"]),
                       model_index, apply)

    print("\n== DB-bound groups (preserve names, leave items alone) ==")
    for name in DB_BOUND_GROUP_NAMES:
        g = by_name.get(name)
        if g is None:
            print(f"  ! WARNING: expected DB-bound group {name!r} is missing")
        else:
            print(f"  = preserving {name!r} ({len(g.get('items') or [])} items)")

    print("\n== Old groups marked for retirement ==")
    for name in RETIRE_GROUPS:
        g = by_name.get(name)
        if g is None:
            continue
        if delete_old and apply:
            print(f"  - delete {name!r} (id={g['id']})")
            client.group_delete(g["id"])
        else:
            action = "would delete" if apply else "would delete (dry-run)"
            if not delete_old:
                action = "skip (set OCTOPUS_DELETE_OLD=1 to remove)"
            print(f"  - {name!r}: {action}")

    print("\nDone.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
