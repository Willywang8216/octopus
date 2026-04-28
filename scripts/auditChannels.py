#!/usr/bin/env python3
"""
Octopus channel audit and tagging.

Classifies every channel by historical success rate, applies a quality tag
to its name, encodes audit metadata in the first key's remark, and disables
DEAD/ZOMBIE channels currently enabled. NEVER deletes channels — deletion
is destructive (loses keys + custom config) and is left to the operator.

Quality bands:
  ALIVE     >=20% success and >=50 total req
  FLAKY     5-20% success
  NEW       <50 total req (untested)
  DEAD      <5% success and >=500 total req
  ZOMBIE    0% success and >=5000 total req

Channel name is prefixed with [<BAND>] (existing prefix replaced if present).
First channel-key remark is set to a structured string:
  "quality=<band> success=<n> failed=<n> rate=<pct>% audited=<date>"

Re-running is a no-op once tags match the current data.

Env:
  OCTOPUS_BASE_URL, OCTOPUS_USERNAME, OCTOPUS_PASSWORD
  OCTOPUS_APPLY=1   apply changes (otherwise dry-run)
"""

import datetime
import json
import os
import re
import sys
import urllib.error
import urllib.request
from typing import Any

BAND_PREFIX_RE = re.compile(r"^\[(ALIVE|FLAKY|NEW|DEAD|ZOMBIE)\]\s*")

DEAD_TOTAL_THRESHOLD = 500
ZOMBIE_TOTAL_THRESHOLD = 5000


def classify(channel: dict) -> tuple[str, float, int]:
    s = channel.get("stats") or {}
    succ = int(s.get("request_success") or 0)
    fail = int(s.get("request_failed") or 0)
    total = succ + fail
    if total == 0:
        return ("NEW", 0.0, 0)
    rate = succ / total
    if total >= ZOMBIE_TOTAL_THRESHOLD and succ == 0:
        return ("ZOMBIE", rate, total)
    if total >= DEAD_TOTAL_THRESHOLD and rate < 0.05:
        return ("DEAD", rate, total)
    if total < 50:
        return ("NEW", rate, total)
    if rate >= 0.20:
        return ("ALIVE", rate, total)
    return ("FLAKY", rate, total)


class Client:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")
        self.token: str | None = None

    def _req(self, method: str, path: str, body: dict | None = None) -> Any:
        url = f"{self.base_url}{path}"
        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": "Mozilla/5.0",
        }
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, data=data, method=method, headers=headers)
        try:
            with urllib.request.urlopen(req) as r:
                payload = json.loads(r.read().decode())
        except urllib.error.HTTPError as e:
            raise SystemExit(f"HTTP {e.code} on {method} {path}: {e.read().decode(errors='replace')}")
        if isinstance(payload, dict) and payload.get("code") not in (None, 0, 200):
            raise SystemExit(f"API error on {method} {path}: {payload}")
        return payload.get("data") if isinstance(payload, dict) else payload

    def login(self, u: str, p: str) -> None:
        d = self._req("POST", "/api/v1/user/login", {"username": u, "password": p})
        self.token = d["token"]

    def channels(self) -> list[dict]:
        return self._req("GET", "/api/v1/channel/list") or []

    def update_channel(self, req: dict) -> Any:
        return self._req("POST", "/api/v1/channel/update", req)

    def enable_channel(self, cid: int, enabled: bool) -> Any:
        return self._req("POST", "/api/v1/channel/enable", {"id": cid, "enabled": enabled})


def desired_name(current: str, band: str) -> str:
    cleaned = BAND_PREFIX_RE.sub("", current).strip()
    return f"[{band}] {cleaned}".strip()


def desired_remark(band: str, succ: int, fail: int, rate: float) -> str:
    today = datetime.date.today().isoformat()
    return (f"quality={band} success={succ} failed={fail} "
            f"rate={rate * 100:.1f}% audited={today}")


def main() -> int:
    base_url = os.environ.get("OCTOPUS_BASE_URL")
    user = os.environ.get("OCTOPUS_USERNAME")
    pw = os.environ.get("OCTOPUS_PASSWORD")
    apply = os.environ.get("OCTOPUS_APPLY") == "1"
    if not (base_url and user and pw):
        print("ERROR: set OCTOPUS_BASE_URL, OCTOPUS_USERNAME, OCTOPUS_PASSWORD", file=sys.stderr)
        return 2

    print(f"Mode: {'APPLY' if apply else 'DRY-RUN'}")
    print(f"Target: {base_url}")
    c = Client(base_url)
    c.login(user, pw)
    chs = c.channels()
    print(f"Loaded {len(chs)} channels.\n")

    summary: dict[str, list] = {b: [] for b in ("ALIVE", "FLAKY", "NEW", "DEAD", "ZOMBIE")}

    for ch in chs:
        band, rate, total = classify(ch)
        s = ch.get("stats") or {}
        succ = int(s.get("request_success") or 0)
        fail = int(s.get("request_failed") or 0)
        cur_name = ch.get("name", "")
        new_name = desired_name(cur_name, band)
        keys = ch.get("keys") or []
        first_key = keys[0] if keys else None
        cur_remark = (first_key or {}).get("remark", "") if first_key else ""
        new_remark = desired_remark(band, succ, fail, rate)

        should_disable = (
            band in ("ZOMBIE", "DEAD") and ch.get("enabled") is True
        )

        update_payload: dict[str, Any] = {"id": ch["id"]}
        actions: list[str] = []

        if cur_name != new_name:
            actions.append(f"rename '{cur_name}' -> '{new_name}'")
            update_payload["name"] = new_name

        if first_key is not None and cur_remark != new_remark:
            actions.append(f"remark key#{first_key['id']}")
            update_payload["keys_to_update"] = [{
                "id": first_key["id"],
                "remark": new_remark,
            }]

        summary[band].append((ch["id"], cur_name, succ, fail, rate, should_disable))

        if len(update_payload) > 1:
            print(f"ch{ch['id']:>3} [{band}] {' / '.join(actions)}")
            if apply:
                c.update_channel(update_payload)

        if should_disable:
            print(f"ch{ch['id']:>3} [{band}] disable (was enabled)")
            if apply:
                c.enable_channel(ch["id"], False)

    print("\n=== Summary ===")
    for band in ("ALIVE", "FLAKY", "NEW", "DEAD", "ZOMBIE"):
        rows = summary[band]
        if not rows:
            continue
        print(f"\n{band} ({len(rows)}):")
        for cid, name, succ, fail, rate, dis in sorted(rows, key=lambda r: -r[4]):
            mark = " -> DISABLE" if dis else ""
            print(f"  ch{cid:>3}  rate={rate * 100:5.1f}%  total={succ+fail:>6}  {name[:40]}{mark}")

    print("\nDone." if apply else "\nDry-run complete. Re-run with OCTOPUS_APPLY=1 to apply.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
