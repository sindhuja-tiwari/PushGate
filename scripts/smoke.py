#!/usr/bin/env python3
"""Week 1 smoke test: start the fleet, push three bad files with no gate
(baseline B0, push-all), and check that each one breaks exactly the releases
it should. Then roll back and check the fleet recovers.

  scenario      bad file                                 should crash
  cloudflare    220 features (duplicate rows)             bot-proxy, every release
  crowdstrike   21-field rule template                    bot-proxy v41-v43, not v44
  google        quota policy with blank quota block       go-policy v43-v44, not v41-v42

Run with the producer's venv:  .venv/bin/python scripts/smoke.py [--scale 0.2]
"""

from __future__ import annotations

import argparse
import json
import os
import signal
import subprocess
import sys
import time
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "state", "out")
REGISTRY = "http://127.0.0.1:19000/registry"
PY = sys.executable


def sh(*args: str, quiet: bool = True) -> str:
    r = subprocess.run(args, cwd=ROOT, check=True, capture_output=True, text=True)
    if not quiet:
        print(r.stdout, end="")
    return r.stdout


def producer(*args: str) -> None:
    sh(PY, "producer/producer.py", *args)


def registry() -> list[dict]:
    with urllib.request.urlopen(REGISTRY, timeout=10) as r:
        return json.load(r)["instances"]


def restarts() -> dict[str, int]:
    return {i["id"]: i["restarts"] for i in registry()}


def crashed_share(before: dict[str, int], after: list[dict]) -> dict[str, float]:
    """Share of instances per consumer/release that restarted at least once."""
    hit: dict[str, list[int]] = {}
    for i in after:
        k = f"{i['consumer']}/v{i['version']}"
        hit.setdefault(k, [0, 0])
        hit[k][1] += 1
        if i["restarts"] > before.get(i["id"], 0):
            hit[k][0] += 1
    return {k: v[0] / v[1] for k, v in sorted(hit.items())}


def make_files() -> dict[str, str]:
    os.makedirs(OUT, exist_ok=True)
    db = os.path.join(ROOT, "state", "smoke.duckdb")
    f = {k: os.path.join(OUT, v) for k, v in {
        "feature_good": "feature-good.json",
        "feature_dup": "feature-dup.json",
        "feature_21": "feature-21field.json",
        "policy_good": "policy-good.json",
        "policy_blank": "policy-blank.json",
    }.items()}
    producer("init", "--db", db)
    producer("features", "--db", db, "--out", f["feature_good"])
    producer("features", "--db", db, "--out", f["feature_21"], "--extra-field", "v1", "--extra-count", "5")
    producer("grant-r0", "--db", db)
    producer("features", "--db", db, "--out", f["feature_dup"])
    producer("revoke-r0", "--db", db)
    producer("policy", "--out", f["policy_good"])
    producer("policy", "--out", f["policy_blank"], "--blank-quota", "3")
    return f


SCENARIOS = [
    # name, consumer, bad file key, good file key, {release: should crash}
    ("cloudflare", "bot-proxy", "feature_dup", "feature_good", {41: True, 42: True, 43: True, 44: True}),
    ("crowdstrike", "bot-proxy", "feature_21", "feature_good", {41: True, 42: True, 43: True, 44: False}),
    ("google", "go-policy", "policy_blank", "policy_good", {41: False, 42: False, 43: True, 44: True}),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--scale", type=float, default=1.0, help="fleet size multiplier (1.0 = 500 instances)")
    ap.add_argument("--rate", type=int, default=2000, help="requests per second")
    ap.add_argument("--settle", type=float, default=6.0, help="seconds to watch after each bad push")
    a = ap.parse_args()

    files = make_files()
    fleet = subprocess.Popen(
        [os.path.join(ROOT, "bin", "fleet"), "-config", "deploy/fleet.json", "-fresh", "-scale", str(a.scale)],
        cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    load = None
    failures = 0
    try:
        sh("bin/pgctl", "wait", "-timeout", "90s", quiet=False)
        n = len(registry())
        jsonl = os.path.join(ROOT, "state", "smoke-load.jsonl")
        if os.path.exists(jsonl):
            os.remove(jsonl)
        load = subprocess.Popen(
            [os.path.join(ROOT, "bin", "loadgen"), "-rate", str(a.rate), "-quiet", "-jsonl", jsonl],
            cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        time.sleep(3)
        print(f"\nfleet of {n} instances under {a.rate} req/s. Pushing with no gate (B0).\n")

        for name, consumer, bad, good, expect in SCENARIOS:
            before = restarts()
            t0 = time.time()
            sh("bin/pgctl", "push", "-consumer", consumer, "-file", files[bad])
            time.sleep(a.settle)
            share = crashed_share(before, registry())
            print(f"[{name}] pushed {os.path.basename(files[bad])} to every {consumer}")
            ok = True
            for v, should in sorted(expect.items()):
                got = share.get(f"{consumer}/v{v}", 0.0)
                passed = got >= 0.9 if should else got == 0.0
                ok &= passed
                want = "crash" if should else "survive"
                print(f"    {consumer} v{v}: {got:6.1%} crashed   expected {want:7s}  {'ok' if passed else 'FAIL'}")
            others = {k: v for k, v in share.items() if not k.startswith(consumer + "/") and v > 0}
            if others:
                ok = False
                print(f"    unexpected crashes in other consumers: {others}")
            sh("bin/pgctl", "push", "-consumer", consumer, "-file", files[good])
            sh("bin/pgctl", "wait", "-timeout", "90s")
            print(f"    rolled back; all instances up again after {time.time() - t0 - a.settle:.1f}s")
            print(f"    {'PASS' if ok else 'FAIL'}\n")
            failures += 0 if ok else 1
            time.sleep(2)

        print(sh("bin/pgctl", "crashes"))
    finally:
        if load:
            load.send_signal(signal.SIGINT)
            load.wait(timeout=10)
        fleet.send_signal(signal.SIGINT)
        fleet.wait(timeout=30)

    print("smoke:", "PASS" if failures == 0 else f"FAIL ({failures} scenario(s))")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
