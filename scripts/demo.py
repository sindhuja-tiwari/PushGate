#!/usr/bin/env python3
"""The PushGate demo: two bad files, pushed the way the incidents pushed them
and then through PushGate, on a live 100-instance fleet.

  Act 1  Cloudflare-shaped file (every feature twice) under B0, then B3.
  Act 2  Google-shaped file (blank quota block) under B2, then B3. On fleet
         layout 1, rings 0 and 1 hold only safe releases, so B2 carries the
         file to the whole fleet before its alarm fires.

It prints what it runs as it goes, and records every command, every output
line and a registry snapshot every 200 ms to state/demo/record.jsonl.
scripts/render_demo.py turns that recording into docs/demo.mp4.

Run from the repository root after `make build`:  python3 scripts/demo.py
"""

import json
import os
import signal
import subprocess
import sys
import threading
import time
import urllib.request

REGISTRY = "http://127.0.0.1:19000/registry"
LOADGEN = "127.0.0.1:19100"
OUT_DIR = "state/demo"


class Recorder:
    def __init__(self, path):
        self.f = open(path, "w")
        self.t0 = time.monotonic()
        self.lock = threading.Lock()
        self.stop = threading.Event()

    def now(self):
        return round(time.monotonic() - self.t0, 3)

    def write(self, rec):
        rec["t"] = self.now()
        with self.lock:
            self.f.write(json.dumps(rec) + "\n")
            self.f.flush()

    def poll(self):
        while not self.stop.is_set():
            try:
                with urllib.request.urlopen(REGISTRY, timeout=1) as r:
                    snap = json.load(r)
                insts = [
                    {k: i.get(k) for k in ("id", "consumer", "version", "state", "config_version", "restarts")}
                    for i in snap.get("instances", [])
                ]
                self.write({"kind": "fleet", "instances": insts})
            except Exception:
                pass
            self.stop.wait(0.2)


def say(rec, text):
    print(f"\n\033[1m{text}\033[0m", flush=True)
    rec.write({"kind": "caption", "text": text})


def run(rec, argv, show=None):
    shown = show or " ".join(argv)
    print(f"$ {shown}", flush=True)
    rec.write({"kind": "cmd", "text": shown})
    p = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    for line in p.stdout:
        line = line.rstrip("\n")
        print(line, flush=True)
        rec.write({"kind": "out", "text": line})
    return p.wait()


def wait_up(timeout=60):
    subprocess.run(["bin/pgctl", "wait", "-timeout", f"{timeout}s"], stdout=subprocess.DEVNULL, check=False)


def main():
    os.makedirs(OUT_DIR, exist_ok=True)
    procs = []

    def cleanup(*_):
        for p in reversed(procs):
            if p.poll() is None:
                p.send_signal(signal.SIGINT)
                try:
                    p.wait(10)
                except subprocess.TimeoutExpired:
                    p.kill()

    signal.signal(signal.SIGTERM, lambda *a: (cleanup(), sys.exit(1)))
    try:
        print("Starting a 100-instance fleet (layout 1) and 1,000 requests/s of traffic ...", flush=True)
        fleet = subprocess.Popen(
            ["bin/fleet", "-config", "deploy/fleet.json", "-fresh", "-scale", "0.2", "-seed", "1"],
            stdout=open(f"{OUT_DIR}/fleet.log", "w"), stderr=subprocess.STDOUT)
        procs.append(fleet)
        wait_up(90)
        load = subprocess.Popen(
            ["bin/loadgen", "-rate", "1000", "-quiet", "-listen", LOADGEN, "-record", "state/traffic.txt"],
            stdout=open(f"{OUT_DIR}/loadgen.log", "w"), stderr=subprocess.STDOUT)
        procs.append(load)
        time.sleep(7)  # loadgen writes its first traffic sample after 5 s
        for c, f in (("bot-proxy", "seed/feature.json"), ("go-policy", "seed/policy.json")):
            subprocess.run(["bin/rollout", "set-lkg", "-consumer", c, "-file", f], check=True, stdout=subprocess.DEVNULL)

        rec = Recorder(f"{OUT_DIR}/record.jsonl")
        poller = threading.Thread(target=rec.poll, daemon=True)
        poller.start()
        push = ["bin/rollout", "-loadgen", "http://" + LOADGEN, "push"]

        say(rec, "A fleet of 100 instances: 80 bot-proxy, 20 go-policy, releases v41 to v44.")
        time.sleep(3)

        say(rec, "Act 1. A Cloudflare-shaped file: every feature listed twice. B0 pushes it everywhere.")
        time.sleep(1)
        run(rec, push + ["-strategy", "b0", "-consumer", "bot-proxy", "-file", "gate/testdata/feature-dup.json"],
            "rollout push -strategy b0 -consumer bot-proxy -file feature-dup.json")
        time.sleep(6)
        say(rec, "Every bot-proxy instance crashes on load. Roll back by hand.")
        run(rec, ["bin/rollout", "rollback", "-consumer", "bot-proxy"], "rollout rollback -consumer bot-proxy")
        wait_up()
        time.sleep(2)

        say(rec, "The same file through PushGate (B3).")
        run(rec, push + ["-strategy", "b3", "-consumer", "bot-proxy", "-file", "gate/testdata/feature-dup.json"],
            "rollout push -strategy b3 -consumer bot-proxy -file feature-dup.json")
        time.sleep(3)

        say(rec, "Act 2. A Google-shaped file: three policies with a blank quota. Only v43 and v44 crash on it. B2 rolls out in rings 1%, 10%, 100%.")
        time.sleep(1)
        run(rec, push + ["-strategy", "b2", "-bake", "5s", "-consumer", "go-policy", "-file", "gate/testdata/policy-blank.json"],
            "rollout push -strategy b2 -bake 5s -consumer go-policy -file policy-blank.json")
        wait_up()
        time.sleep(2)

        say(rec, "Rings 0 and 1 held only v41 and v42, so B2 saw nothing wrong until 100%. The same file through PushGate:")
        run(rec, push + ["-strategy", "b3", "-bake", "5s", "-consumer", "go-policy", "-file", "gate/testdata/policy-blank.json"],
            "rollout push -strategy b3 -consumer go-policy -file policy-blank.json")
        time.sleep(3)
        say(rec, "PushGate tested every deployed release before any instance got the file.")
        time.sleep(2)
        rec.stop.set()
        poller.join()
        print(f"\nRecording: {OUT_DIR}/record.jsonl", flush=True)
    finally:
        cleanup()


if __name__ == "__main__":
    main()
