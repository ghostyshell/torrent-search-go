#!/usr/bin/env python3
"""Validate studio catalog responses on prod addon + Go backend."""

import json
import ssl
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

ADDON = "https://stremio-tpb-porn.sliplane.app"
GO = "https://torrent-search-go.sliplane.app"
CTX = ssl.create_default_context()
TIMEOUT = 45


def fetch(url: str) -> tuple[int, int, float, str]:
    req = urllib.request.Request(url, headers={"Accept": "application/json", "User-Agent": "studio-validate/1"})
    t0 = time.time()
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT, context=CTX) as r:
            data = json.loads(r.read().decode())
        dt = time.time() - t0
        return len(data.get("metas", [])), r.status, dt, ""
    except Exception as e:
        return -1, 0, time.time() - t0, str(e)


def main() -> int:
    manifest_url = f"{ADDON}/manifest.json"
    with urllib.request.urlopen(manifest_url, timeout=30, context=CTX) as r:
        manifest = json.load(r)

    studio_ids = [
        c["id"]
        for c in manifest.get("catalogs", [])
        if c.get("id", "").startswith("xxx_studio_")
    ]
    # Sample a few known-bad + all unique studio bases (4k top only for speed)
    priority = {
        "xxx_studio_groobygirls_top",
        "xxx_studio_groobygirls_fhd_top",
        "xxx_studio_tokyo_hot_fhd_top",
        "xxx_studio_brazzersexxtra_top",
    }
    bases = sorted({cid.rsplit("_", 1)[0] for cid in studio_ids if cid.endswith("_top")})
    top_ids = [b + "_top" for b in bases]
    ids = list(dict.fromkeys([*priority, *top_ids]))

    print(f"Checking {len(ids)} studio top catalogs on {ADDON}\n")
    empty = []
    errors = []
    slow = []

    def check(cid: str):
        url = f"{ADDON}/catalog/Porn/{cid}.json"
        n, status, dt, err = fetch(url)
        return cid, n, status, dt, err

    with ThreadPoolExecutor(max_workers=6) as pool:
        futures = {pool.submit(check, cid): cid for cid in ids}
        for fut in as_completed(futures):
            cid, n, status, dt, err = fut.result()
            label = "OK" if n > 0 else ("ERR" if err else "EMPTY")
            print(f"{label:5} {n:3}  {dt:5.1f}s  {cid}" + (f"  ({err[:60]})" if err else ""))
            if err:
                errors.append(cid)
            elif n == 0:
                empty.append(cid)
            elif dt > 20:
                slow.append(cid)

    print(f"\nSummary: {len(ids) - len(empty) - len(errors)}/{len(ids)} non-empty")
    if empty:
        print(f"Empty ({len(empty)}): {', '.join(empty[:20])}" + (" ..." if len(empty) > 20 else ""))
    if errors:
        print(f"Errors ({len(errors)}): {', '.join(errors[:10])}")
    if slow:
        print(f"Slow >20s ({len(slow)}): {', '.join(slow[:10])}")
    return 1 if empty or errors else 0


if __name__ == "__main__":
    sys.exit(main())
