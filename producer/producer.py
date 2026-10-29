#!/usr/bin/env python3
"""PushBench producer: builds the config files the fleet consumes.

Feature files (for bot-proxy) are generated from a DuckDB database the way
Cloudflare's Bot Management file was generated from ClickHouse: by reading
column metadata for the feature table and joining it with feature weights.

The metadata query has the same flaw as the one in Cloudflare's postmortem.
It filters by table name but not by database (schema). That is harmless until
a second schema with a same-named table becomes visible. ``grant-r0``
simulates that permission change. After it, every feature appears twice, the
file more than doubles, and consumers with a 200-feature preallocation crash.

Commands
  init       create the database with N features (default 110)
  features   write one feature file
  run        write a feature file every --interval seconds
  grant-r0   make the r0 schema's copy of the table visible (the bad change)
  revoke-r0  undo grant-r0
  policy     write a quota policy file (for go-policy)

Examples
  python3 producer/producer.py init --db state/producer.duckdb
  python3 producer/producer.py features --db state/producer.duckdb --out seed/feature.json
  python3 producer/producer.py grant-r0 --db state/producer.duckdb
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import random
import sys
import tempfile
import time
from datetime import datetime, timezone

try:
    import duckdb
except ImportError:  # pragma: no cover
    sys.exit("producer: duckdb is not installed. Run `make producer-deps` or `pip install -r producer/requirements.txt`.")

# Must match the request vocabulary in fleet/cmd/loadgen/main.go.
USER_AGENTS = ["chrome", "safari", "firefox", "curl", "python-requests", "go-http-client", "headless-chrome", "bot"]
METHODS = ["GET", "POST", "HEAD"]
COUNTRIES = ["US", "IN", "DE", "BR", "JP", "NG"]
ASN_CLASSES = ["residential", "mobile", "hosting", "university"]
BOTLIKE = {"curl", "python-requests", "go-http-client", "headless-chrome", "bot"}
TEMPLATE_FIELDS = 20  # fields per rule template the current consumers expect

# The flawed query. Note the missing `AND c.table_schema = 'main'`.
FEATURE_QUERY = """
SELECT c.column_name AS name, w.weight, w.fields
FROM information_schema.columns AS c
JOIN main.feature_weights AS w ON w.name = c.column_name
WHERE c.table_name = 'http_request_features'
ORDER BY c.ordinal_position, c.table_schema
"""


def write_atomic(path: str, data: str) -> None:
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(os.path.abspath(path)), prefix=".tmp-")
    with os.fdopen(fd, "w") as f:
        f.write(data)
    os.replace(tmp, path)


def make_template(rng: random.Random) -> list[str]:
    fields = ["*"] * TEMPLATE_FIELDS
    fields[0] = rng.choice(USER_AGENTS)
    if rng.random() < 0.5:
        fields[1] = rng.choice(METHODS)
    if rng.random() < 0.4:
        fields[2] = rng.choice(COUNTRIES)
    if rng.random() < 0.4:
        fields[3] = rng.choice(ASN_CLASSES)
    return fields


def cmd_init(a: argparse.Namespace) -> None:
    rng = random.Random(a.seed)
    if os.path.exists(a.db):
        os.remove(a.db)
    os.makedirs(os.path.dirname(os.path.abspath(a.db)), exist_ok=True)
    con = duckdb.connect(a.db)
    names = [f"feat_{i:03d}" for i in range(a.features)]
    cols = ", ".join(f"{n} DOUBLE" for n in names)
    con.execute(f"CREATE TABLE main.http_request_features (request_id BIGINT, {cols})")
    con.execute("CREATE TABLE main.feature_weights (name VARCHAR PRIMARY KEY, weight DOUBLE, fields VARCHAR)")
    rows = []
    for n in names:
        t = make_template(rng)
        w = rng.uniform(0.5, 2.5) if t[0] in BOTLIKE else -rng.uniform(0.2, 1.5)
        rows.append((n, round(w, 3), json.dumps(t)))
    con.executemany("INSERT INTO main.feature_weights VALUES (?, ?, ?)", rows)
    con.close()
    print(f"producer: created {a.db} with {a.features} features")


def build_features(db: str, extra_field: str | None = None, extra_count: int = 0) -> dict:
    con = duckdb.connect(db, read_only=True)
    rows = con.execute(FEATURE_QUERY).fetchall()
    con.close()
    features = [{"name": n, "weight": w, "fields": json.loads(f)} for (n, w, f) in rows]
    if extra_field is not None:
        # The CrowdStrike shape: a new rule template with one more field than
        # older consumers have input slots for. Only the first `extra_count`
        # rules use it, and only requests that reach that field trigger the
        # out-of-bounds read.
        for f in features[:extra_count]:
            f["fields"] = [f["fields"][0]] + ["*"] * (TEMPLATE_FIELDS - 1) + [extra_field]
    digest = hashlib.sha256(json.dumps(features, sort_keys=True).encode()).hexdigest()[:10]
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    return {"schema": "bot-features/1", "config_version": f"feat-{stamp}-{digest}", "features": features}


def cmd_features(a: argparse.Namespace) -> None:
    doc = build_features(a.db, a.extra_field, a.extra_count)
    write_atomic(a.out, json.dumps(doc, indent=1))
    print(f"producer: wrote {a.out}: {len(doc['features'])} features ({doc['config_version']})")


def cmd_run(a: argparse.Namespace) -> None:
    print(f"producer: writing {a.out} every {a.interval}s (Ctrl-C to stop)")
    try:
        while True:
            cmd_features(a)
            time.sleep(a.interval)
    except KeyboardInterrupt:
        pass


def cmd_grant(a: argparse.Namespace) -> None:
    con = duckdb.connect(a.db)
    con.execute("CREATE SCHEMA IF NOT EXISTS r0")
    con.execute("CREATE OR REPLACE TABLE r0.http_request_features AS SELECT * FROM main.http_request_features LIMIT 0")
    con.close()
    print("producer: r0.http_request_features is now visible to the feature query")


def cmd_revoke(a: argparse.Namespace) -> None:
    con = duckdb.connect(a.db)
    con.execute("DROP SCHEMA IF EXISTS r0 CASCADE")
    con.close()
    print("producer: r0 schema removed")


def cmd_policy(a: argparse.Namespace) -> None:
    rng = random.Random(a.seed)
    apis = ["storage.read", "storage.write", "compute.list", "iam.get"]
    policies = []
    for p in range(a.projects):
        for api in apis:
            enforce = rng.random() < 0.3
            pol = {"project": f"p-{p:03d}", "api": api, "allow": rng.random() < 0.9, "enforce_quota": enforce}
            pol["quota"] = {"limit_per_min": rng.choice([600, 1200, 6000]), "burst": 50} if enforce else None
            policies.append(pol)
    # The Google Cloud shape: rows that enable enforcement but leave the
    # quota block blank.
    for i in range(a.blank_quota):
        policies[i]["enforce_quota"] = True
        policies[i]["quota"] = None
    digest = hashlib.sha256(json.dumps(policies, sort_keys=True).encode()).hexdigest()[:10]
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    doc = {"schema": "quota-policy/1", "config_version": f"pol-{stamp}-{digest}", "policies": policies}
    write_atomic(a.out, json.dumps(doc, indent=1))
    print(f"producer: wrote {a.out}: {len(policies)} policies, {a.blank_quota} with blank quota")


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)

    s = sub.add_parser("init")
    s.add_argument("--db", default="state/producer.duckdb")
    s.add_argument("--features", type=int, default=110)
    s.add_argument("--seed", type=int, default=7)
    s.set_defaults(fn=cmd_init)

    for name, fn in (("features", cmd_features), ("run", cmd_run)):
        s = sub.add_parser(name)
        s.add_argument("--db", default="state/producer.duckdb")
        s.add_argument("--out", default="state/out/feature.json")
        s.add_argument("--extra-field", default=None, help="append this value as field 21 on some rules (CrowdStrike shape)")
        s.add_argument("--extra-count", type=int, default=5, help="how many rules get the extra field")
        if name == "run":
            s.add_argument("--interval", type=float, default=300)
        s.set_defaults(fn=fn)

    for name, fn in (("grant-r0", cmd_grant), ("revoke-r0", cmd_revoke)):
        s = sub.add_parser(name)
        s.add_argument("--db", default="state/producer.duckdb")
        s.set_defaults(fn=fn)

    s = sub.add_parser("policy")
    s.add_argument("--out", default="state/out/policy.json")
    s.add_argument("--projects", type=int, default=50)
    s.add_argument("--blank-quota", type=int, default=0)
    s.add_argument("--seed", type=int, default=7)
    s.set_defaults(fn=cmd_policy)

    a = p.parse_args()
    a.fn(a)


if __name__ == "__main__":
    main()
