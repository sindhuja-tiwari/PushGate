# The gate (B3)

B3 is PushGate: B2's rings, bake times, alarms and rollback, with a
pre-exposure gate in front. The gate runs after the format and schema checks
and before any instance sees the file. It has two layers. Any finding with
severity `hold` stops the push and explains why. `warn` and `info` findings
are recorded in the push record and the push goes on to the rings.

Code: `gate/`. Command: `bin/gate check`. Strategy names: `b3` (both layers),
`b3-diff` and `b3-sandbox` (one layer each, for ablations).

## Layer 1: semantic diff

Compares the candidate with the last known good file from the rollout store.
It needs no consumer code, so it is cheap, but it can only judge how the file
changed, not whether a consumer can handle it.

| Check | bot-proxy (features) | go-policy (policies) | Severity |
|---|---|---|---|
| Row count | features | policies | hold above ±50%, warn above ±20% |
| Duplicate keys | feature name | project + API | hold if there are more than in last known good |
| New shape | rules using a template width the last known good never had | – | warn |
| New nulls | – | `enforce_quota` rows with a blank quota block | warn if more than in last known good |
| Drift (PSI) | rule weights | quota limits | warn above 0.25 |
| Churn | share of feature names not in last known good | – | warn above 50% |
| Allow flips | – | share of policies whose allow/deny changed | warn above 20% |

The rule behind the severities: **hold only on what is almost never
legitimate, warn on what sometimes is.** A file that doubles overnight, or
repeats every key, is almost always a producer bug. A new 21-field rule or a
policy without a quota can be exactly what the author meant, and only running
the consumer shows whether it is fatal. That is why the diff alone holds the
Cloudflare file but only warns on the CrowdStrike and Google files.

PSI (population stability index) bins the last known good values at their
deciles and sums `(cur - base) * ln(cur / base)` over the bins, with a floor
of 1e-4 per bin. Above 0.25 is the usual "significant shift" threshold.

All thresholds are in `gate.Budgets` (`gate/diff.go`).

## Layer 2: consumer-in-the-loop sandbox

1. Read the fleet registry and list the releases of this consumer that are
   live right now, with how many instances run each.
2. For each release, start that exact binary (`bin/<consumer>/v<NN>`) twice
   in parallel: once with the candidate file, once with the last known good.
   Each is a separate process on a free local port with a private copy of the
   file, so nothing touches the fleet.
3. Wait for `/healthz`. A process that exits first crashed on load; its
   stderr is parsed for the panic (Rust `panicked at` or Go `panic:` plus the
   first non-runtime frame).
4. Replay up to 1,000 requests from the traffic sample, one at a time. If a
   request fails and the process exits, that request killed it, and the
   report names it.
5. Compare with the last known good run: error share, p99 latency, resident
   memory and the answer each request got (`bot` for bot-proxy, `allow` for
   go-policy).

| Result | Severity |
|---|---|
| Crash on load or on a replayed request | hold |
| More than 1% of replayed requests fail | hold |
| p99 more than 3x last known good and more than 20 ms above it | hold |
| More than 10% of answers differ from last known good | warn (changed behavior is often the point) |
| Memory more than 2x last known good | warn |

The sandbox runs under a one-minute timeout. On a 4-core machine one check
takes about 0.1 to 0.4 s for four releases, because every release and both
files run in parallel.

### Where the traffic comes from

`loadgen -record state/traffic.txt` keeps a uniform reservoir sample
(algorithm R) of the requests it sends, 5,000 by default, written every 5 s.
Each line is `consumer<TAB>/path?query`. pushbench starts loadgen with
`-record` automatically. Without a sample the sandbox still checks that every
release loads the file, and warns that it replayed nothing.

## The report

Every check produces a report, stored in the push record under
`checks.gate`. `bin/gate check` prints it:

```
HOLD  (bot-proxy, candidate feat-...0e, last known good feat-...00, 199 ms)

  info  diff     row-count       feature count 110 vs 110 in last known good (+0%)
  warn  diff     new-shape       5 rules use a 21-field template; last known good only has [20]-field templates
  info  diff     drift           weight distribution PSI 0.000
  hold  sandbox  replay         v41  v41 (20 instances) crashed on replayed request 2 of 1000 (/score?f0=go-http-client&f1=GET&f2=JP&f3=university&f4=v1...): panicked at src/main.rs:108:20: index out of bounds: the len is 20 but the index is 20
  ...

  release  instances  result            requests  errors  p99 ms (lkg)   output diff
  v41      20         crashed on #2     1         0        0.67 ( 0.97)     0.0%
  v42      60         crashed on #2     1         0        0.15 ( 0.89)     0.0%
  v43      120        crashed on #2     1         0        0.42 ( 1.08)     0.0%
  v44      200        ok                1000      0        0.76 ( 1.00)     0.0%

Held: v41, v42, v43 (200 of 400 instances) crashes on replayed traffic: panicked at src/main.rs:108:20: index out of bounds: the len is 20 but the index is 20 (also 1 warning(s): 5 rules use a 21-field template; last known good only has [20]-field templates)
```

The last line is the summary a held push carries. Releases that failed the
same way are grouped, and it says how much of the fleet would have gone down.

## Usage

```bash
bin/loadgen -rate 2000 -record state/traffic.txt &      # traffic plus a sample to replay
bin/gate check -consumer bot-proxy -file f.json -lkg seed/feature.json
bin/gate check -consumer go-policy -file p.json -layers diff -json

bin/rollout set-lkg -consumer bot-proxy -file seed/feature.json
bin/rollout -loadgen http://127.0.0.1:19100 push -strategy b3 -consumer bot-proxy -file f.json
```

`gate check` exits 0 on pass, 1 on hold and 2 on errors.

## What it cannot see

- **Requests the sample does not contain.** A crash that needs a rare request
  shape is only caught if the sample includes one. A rare-request scenario in
  week 5 measures this.
- **Slow failures.** A leak that takes ten minutes to matter will not show in
  a one-second replay. The rings and alarms behind the gate remain for that.
- **Releases not in the registry.** The gate tests what is deployed now. A
  release that rolls out after the file does is tested on its own rollout.
