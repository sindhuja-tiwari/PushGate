#!/usr/bin/env bash
# Week 1 demo: the Cloudflare-shaped push, end to end, with no gate (B0).
#
# 1. Start the 500-instance fleet and steady traffic.
# 2. The producer writes a normal feature file from DuckDB. Push it everywhere.
# 3. Simulate the database permission change. The same query now returns
#    every feature twice. Push that file everywhere and watch bot-proxy fail.
# 4. Undo the change, push a good file, and watch the fleet recover.
set -euo pipefail
cd "$(dirname "$0")/.."

PY=.venv/bin/python
DB=state/demo.duckdb
OUT=state/out
mkdir -p "$OUT"

step() { printf '\n\033[1m== %s\033[0m\n' "$*"; }

cleanup() {
  [[ -n "${LOAD_PID:-}" ]] && kill -INT "$LOAD_PID" 2>/dev/null || true
  [[ -n "${FLEET_PID:-}" ]] && kill -INT "$FLEET_PID" 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT

step "Starting the fleet (deploy/fleet.json: 400 bot-proxy + 100 go-policy, 4 regions, v41-v44)"
bin/fleet -config deploy/fleet.json -fresh > state/fleet.out 2>&1 &
FLEET_PID=$!
bin/pgctl wait -timeout 90s

step "Starting traffic: 2000 requests/s, one line per second"
bin/loadgen -rate 2000 > state/demo-load.txt 2>&1 &
LOAD_PID=$!
sleep 3

step "Producer writes a normal feature file and B0 pushes it everywhere"
$PY producer/producer.py init --db "$DB"
$PY producer/producer.py features --db "$DB" --out "$OUT/feature.json"
bin/pgctl push -consumer bot-proxy -file "$OUT/feature.json"
sleep 4; tail -n 3 state/demo-load.txt | cut -c1-120

step "Database permission change: r0.http_request_features becomes visible"
$PY producer/producer.py grant-r0 --db "$DB"
$PY producer/producer.py features --db "$DB" --out "$OUT/feature.json"
bin/pgctl push -consumer bot-proxy -file "$OUT/feature.json"
sleep 6; tail -n 6 state/demo-load.txt | cut -c1-120
echo
bin/pgctl crashes

step "Undo the change and push a good file"
$PY producer/producer.py revoke-r0 --db "$DB"
$PY producer/producer.py features --db "$DB" --out "$OUT/feature.json"
bin/pgctl push -consumer bot-proxy -file "$OUT/feature.json"
bin/pgctl wait -timeout 90s
sleep 3; tail -n 3 state/demo-load.txt | cut -c1-120

step "Done. Full traffic log: state/demo-load.txt"
