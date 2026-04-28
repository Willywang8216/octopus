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
TARGET_GROUPS: list[dict[str, Any]] = [
    {
        "name": "chat",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^chat$",
        "purpose": "Default chat/instruct, balanced quality/latency.",
        "members": [
            (r"(?i)qwen3-30b-a3b-instruct-2507$", 10),
            (r"(?i)qwen3-next-80b-a3b-instruct$", 20),
            (r"(?i)qwen3\.5-35b-a3b$", 30),
            (r"(?i)qwen3\.6-35b-a3b$", 30),
            (r"(?i)qwen3-32b$", 40),
            (r"(?i)qwen3\.5-27b$", 50),
        ],
    },
    {
        "name": "chat-flagship",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^chat-flagship$|(?i)(235b-a22b-instruct|397b-a17b|480b-a35b)",
        "purpose": "Hardest tasks, highest quality.",
        "members": [
            (r"(?i)qwen3-235b-a22b-instruct-2507$", 10),
            (r"(?i)qwen3\.5-397b-a17b$", 20),
            (r"(?i)qwen3\.5-122b-a10b$", 30),
            (r"(?i)qwen3-next-80b-a3b-instruct$", 40),
        ],
    },
    {
        "name": "chat-fast",
        "mode": MODE_ROUND_ROBIN,
        "match_regex": r"(?i)^chat-fast$",
        "purpose": "High-throughput / low-latency cheap tier.",
        "members": [
            (r"(?i)qwen3-8b$", 10),
            (r"(?i)qwen3-14b$", 10),
            (r"(?i)qwen3\.5-9b$", 10),
            (r"(?i)qwen3-30b-a3b-instruct-2507$", 20),
        ],
    },
    {
        "name": "code",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^code$|(?i)coder",
        "purpose": "Coding and agentic coding.",
        "members": [
            (r"(?i)qwen3-coder-480b-a35b-instruct$", 10),
            (r"(?i)qwen3-coder-30b-a3b-instruct$", 20),
            (r"(?i)qwen2\.5-coder-32b-instruct$", 30),
        ],
    },
    {
        "name": "reason",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^reason$|(?i)(thinking|qwq)",
        "purpose": "Explicit chain-of-thought / thinking mode.",
        "members": [
            (r"(?i)qwen3-235b-a22b-thinking-2507$", 10),
            (r"(?i)qwen3-next-80b-a3b-thinking$", 20),
            (r"(?i)qwen3-30b-a3b-thinking-2507$", 30),
            (r"(?i)qwq-32b$", 40),
        ],
    },
    {
        "name": "vision",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^vision$|(?i)qwen.*-vl-(?!embed)",
        "purpose": "Image+text VL (no audio).",
        "members": [
            (r"(?i)qwen3-vl-235b-a22b-instruct$", 10),
            (r"(?i)qwen3-vl-32b-instruct$", 20),
            (r"(?i)qwen3-vl-30b-a3b-instruct$", 30),
            (r"(?i)qwen2\.5-vl-72b-instruct$", 40),
            (r"(?i)qwen3-vl-8b-instruct$", 50),
        ],
    },
    {
        "name": "vision-thinking",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^vision-thinking$|(?i)qwen.*-vl-.*thinking",
        "purpose": "VL with explicit reasoning.",
        "members": [
            (r"(?i)qwen3-vl-32b-thinking$", 10),
            (r"(?i)qwen3-vl-30b-a3b-thinking$", 20),
            (r"(?i)qwen3-vl-8b-thinking$", 30),
        ],
    },
    {
        "name": "omni",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^omni$|(?i)omni-30b",
        "purpose": "Audio + vision + text unified.",
        "members": [
            (r"(?i)qwen3-omni-30b-a3b-instruct$", 10),
            (r"(?i)qwen3-omni-30b-a3b-thinking$", 20),
            (r"(?i)qwen3-omni-30b-a3b-captioner$", 30),
        ],
    },
    {
        "name": "audio",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^audio$|(?i)(asr|tts|whisper|speech)",
        "purpose": "ASR / TTS / speech. Falls back to omni.",
        "members": [
            (r"(?i)(asr|tts|whisper|speech)", 10),
            (r"(?i)qwen3-omni-30b-a3b-instruct$", 90),
        ],
    },
    {
        "name": "embed-qwen3",
        "mode": MODE_ROUND_ROBIN,
        "match_regex": r"(?i)^embed-qwen3$|(?i)qwen3.*embedding",
        "purpose": "Ad-hoc Qwen3 embedding work (not DB-bound).",
        "members": [
            (r"(?i)qwen3-embedding-8b$", 10),
            (r"(?i)qwen3-embedding-4b$", 10),
            (r"(?i)qwen3-embedding-0\.6b$", 10),
            (r"(?i)qwen3-vl-embedding-8b$", 20),
        ],
    },
    {
        "name": "embed-bge",
        "mode": MODE_ROUND_ROBIN,
        "match_regex": r"(?i)^embed-bge$|(?i)bge-(?!.*db)",
        "purpose": "Ad-hoc BGE embedding work (not DB-bound).",
        "members": [
            (r"(?i)bge", 10),
        ],
    },
    {
        "name": "rerank",
        "mode": MODE_ROUND_ROBIN,
        "match_regex": r"(?i)^rerank$|(?i)rerank",
        "purpose": "RAG reranker pool.",
        "members": [
            (r"(?i)qwen3-reranker-8b$", 10),
            (r"(?i)qwen3-reranker-4b$", 10),
            (r"(?i)qwen3-reranker-0\.6b$", 10),
            (r"(?i)bce.*rerank|bge.*rerank", 20),
        ],
    },
    {
        "name": "legacy",
        "mode": MODE_FAILOVER,
        "match_regex": r"(?i)^legacy$|(?i)qwen1\.5",
        "purpose": "Quarantine for qwen1.5 and pre-2507 originals. Backwards compat only.",
        "members": [
            (r"(?i)qwen1\.5-110b-chat$", 10),
            (r"(?i)qwen1\.5-32b-chat$", 20),
            (r"(?i)qwen1\.5-14b-chat$", 30),
            (r"(?i)qwen1\.5-1\.8b-chat$", 40),
            (r"(?i)qwen1\.5-0\.5b-chat$", 50),
            (r"(?i)qwen3-235b-a22b$", 60),
            (r"(?i)qwen3-30b-a3b$", 70),
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
        headers = {"Content-Type": "application/json"}
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


def build_model_index(channels: list[dict]) -> dict[str, list[int]]:
    """Map model_name -> [channel_id, ...]. Channel shape is provider-dependent;
    we look at common fields: `models`, `model_list`, `supported_models`."""
    index: dict[str, list[int]] = {}
    for ch in channels:
        cid = ch.get("id") or ch.get("ID")
        if cid is None:
            continue
        models = (
            ch.get("models")
            or ch.get("model_list")
            or ch.get("supported_models")
            or ch.get("Models")
            or []
        )
        if isinstance(models, str):
            models = [m.strip() for m in re.split(r"[,\s]+", models) if m.strip()]
        for m in models:
            if isinstance(m, dict):
                m = m.get("name") or m.get("model") or ""
            if not m:
                continue
            index.setdefault(m, []).append(int(cid))
    return index


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
    model_index = build_model_index(channels)
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
