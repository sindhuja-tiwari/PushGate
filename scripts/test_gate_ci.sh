#!/usr/bin/env bash
# Self-test for `gate ci` and the GitHub Action's spec format: known-bad
# files must be held and known-good files must pass, for a PushBench
# consumer and for the small Python service in examples/ci. Needs `make build`.
set -uo pipefail
cd "$(dirname "$0")/.."

SPEC=examples/ci/pushgate.json
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
fails=0

expect() { # expect <exit code> <consumer> <candidate> <last known good> <label>
  local want=$1 consumer=$2 file=$3 lkg=$4 label=$5
  bin/gate ci -spec "$SPEC" -consumer "$consumer" -files "$file" -lkg "$lkg" -summary "$TMP/summary.md" > "$TMP/out.txt" 2>&1
  local got=$?
  if [ "$got" = "$want" ]; then
    echo "ok    $label (exit $got)"
  else
    echo "FAIL  $label: exit $got, want $want"; cat "$TMP/out.txt"; fails=$((fails + 1))
  fi
}

LIMITS=examples/ci/config/limits.json
python3 - "$LIMITS" "$TMP" <<'PY'
import json, sys
src, tmp = sys.argv[1], sys.argv[2]
d = json.load(open(src))
bad = json.loads(json.dumps(d)); bad["limits"][3]["burst"] = 0; bad["config_version"] = "limits-burst0"
good = json.loads(json.dumps(d)); good["limits"][0]["per_minute"] *= 2; good["config_version"] = "limits-raised"
dup = json.loads(json.dumps(d)); dup["limits"] += dup["limits"]; dup["config_version"] = "limits-dup"
for name, doc in (("bad", bad), ("good", good), ("dup", dup)):
    json.dump(doc, open(f"{tmp}/limits-{name}.json", "w"))
PY

expect 1 ratelimiter "$TMP/limits-bad.json"  "$LIMITS" "ratelimiter: burst 0 crashes release 1 on load"
expect 0 ratelimiter "$TMP/limits-good.json" "$LIMITS" "ratelimiter: a raised limit passes"
expect 1 ratelimiter "$TMP/limits-dup.json"  "$LIMITS" "ratelimiter: every route twice is held by the generic diff"
expect 1 bot-proxy gate/testdata/feature-rare.json  gate/testdata/feature-good.json "bot-proxy: rule pinned to a rare request is held"
expect 1 bot-proxy gate/testdata/feature-trace.json gate/testdata/feature-good.json "bot-proxy: traced rule that leaks on v44 is held"
expect 0 bot-proxy gate/testdata/feature-good.json  gate/testdata/feature-good.json "bot-proxy: unchanged file passes"
expect 1 go-policy gate/testdata/policy-blank.json  gate/testdata/policy-good.json  "go-policy: blank quota is held"

grep -q "PushGate hold" "$TMP/summary.md" || { echo "FAIL  no Markdown summary written"; fails=$((fails + 1)); }
[ "$fails" = 0 ] && echo "all gate ci checks passed"
exit "$fails"
