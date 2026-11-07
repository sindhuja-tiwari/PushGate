#!/usr/bin/env python3
"""ratelimiter: a tiny consumer outside PushBench, to show `gate ci` on any
service that loads a config file and answers HTTP.

It loads a limits file and answers GET /limit?route=/api/x with
{"route": ..., "allowed": ..., "refill_ms": ...}. Release 1 computes the
refill interval as 60000 / burst at load, so a limit with burst 0 crashes
it. Release 2 treats burst 0 as "no bursting" and keeps running.

    server.py --release 1 --port 8080 --config limits.json
"""

import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse


def load(path, release):
    with open(path) as fh:
        doc = json.load(fh)
    table = {}
    for lim in doc["limits"]:
        if release >= 2 and lim["burst"] == 0:
            refill = None
        else:
            refill = 60000 // lim["burst"]  # release 1: ZeroDivisionError on burst 0
        table[lim["route"]] = {"per_minute": lim["per_minute"], "refill_ms": refill}
    return doc.get("config_version", "unknown"), table


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--release", type=int, required=True)
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--config", required=True)
    a = ap.parse_args()
    version, table = load(a.config, a.release)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def reply(self, body, code=200):
            raw = json.dumps(body).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self):
            u = urlparse(self.path)
            if u.path == "/healthz":
                return self.reply({"ok": True, "release": a.release, "config_version": version})
            if u.path == "/limit":
                route = parse_qs(u.query).get("route", [""])[0]
                lim = table.get(route)
                if lim is None:
                    return self.reply({"route": route, "allowed": True, "refill_ms": None})
                return self.reply({"route": route, "allowed": lim["per_minute"] > 0, "refill_ms": lim["refill_ms"]})
            self.reply({"error": "not found"}, 404)

    ThreadingHTTPServer(("127.0.0.1", a.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
