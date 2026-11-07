# PushGate

**Stop a bad config push before it takes down the fleet.**

PushGate is an open-source safety gate for fast-moving config and data pushes. Before a new file reaches your fleet, PushGate loads it into the *actual consumer binary*, at every version currently deployed, replays sampled traffic, and compares the result against the last known good file. Only then does it roll out in rings, with health gates and automatic rollback.

It ships with **PushBench**, a reproducible benchmark of incidents rebuilt from public postmortems, so you can measure how much of a fleet each rollout strategy would have lost.

> **Status: week 5 of 6 done.** The simulated fleet, both consumers at four releases each, the DuckDB producer, the three baselines (B0 push-all, B1 schema, B2 staged rollout with alarms and rollback), **PushGate's pre-exposure gate (B3)** and the **consumer-side last-known-good library** (Go and Rust) work end to end. The full benchmark (10 bad scenarios, 2 good pushes and a 500-file good-push corpus, 5 fleet layouts, confidence intervals) is committed. Next: the write-up and demo. See [Roadmap](#roadmap).

## Results so far

Three public incidents, rebuilt as scenarios on a 500-instance fleet:

| Strategy | What happened |
|---|---|
| **B0 push-all** | The bad file reached every instance in under a second. 46% to 100% of requests failed. |
| **B1 schema** | Caught none of the three. Each file is legal in its format, and the limits it broke live in consumer code. |
| **B2 staged + alarms** | Contained all three to 0.1% to 0.2% of requests, **but only because ring 0 happened to contain a vulnerable release.** On a 100-instance fleet, B2 rolled the Google-shaped file out to 100% and 60% of the fleet crashed. |
| **B3 PushGate** | Held all three before any instance got the file, on both fleet sizes, in 0.2 to 0.3 s per check. Each held push names the releases that crash and the panic. |
| **Last-known-good library** | With the library inside the consumers, even B0 push-all crashed nothing: vulnerable releases refused the file and kept serving the old one. No requests failed. |

Across 10 bad scenarios and 5 fleet layouts (week 5, 100-instance fleet), B3 held 46 of 50 bad pushes before any instance got them (92%, 95% CI 81 to 97%), against 10 of 50 for B2. It held none of 500 safe files (0%, CI 0 to 0.8%) and added 0.3 s to a 10 s staged rollout. The four it missed are the rare-request scenario, which the gate catches only when the traffic sample happens to contain the rare request.

Staged rollout picks ring 0 by instance, not by release, so whether it catches a release-specific bad file is partly luck. Running the file through every deployed consumer version *before* exposure removes the luck. Details in [What week 2 shows](#what-week-2-shows), [What week 3 shows](#what-week-3-shows), [What week 4 shows](#what-week-4-shows) and [What week 5 shows](#what-week-5-shows).

## Why this exists

Three recent large outages followed the same pattern. A *data or config file* passed its format checks, crashed the program that loaded it, and reached every machine faster than a code release would have been allowed to.

| Incident | The file | Why checks missed it | Impact |
|---|---|---|---|
| CrowdStrike, 19 Jul 2024 | Channel File 291 | Sensor expected 20 fields, got 21: out-of-bounds read | ~8.5M Windows machines crashed |
| Google Cloud, 12 Jun 2025 | Quota policy row with blank fields | Code had no null handling and no feature flag | Global API failures, ~7 h to full recovery |
| Cloudflare, 18 Nov 2025 | Bot Management feature file | Duplicate rows doubled the file past a 200-feature preallocation | Widespread 5xx errors, ~6 h to recovery |

In every case the file was well formed. The failure only appeared when the **real consumer** loaded it. Schema validation alone would have caught none of them. Running the consumer would have caught all three.

## What exists today, and the gap

| Tool | Checks | When |
|---|---|---|
| JSON Schema / validators | Shape | Before rollout |
| AWS AppConfig, Argo Rollouts, Flagger | Health (alarms) | After exposure |
| ctest (OSDI 2020) | Hand-edited config values via unit tests | Before rollout, Java systems |
| **PushGate** | **Behavior, in the real consumer, at every live version** | **Before exposure** |

Large companies almost certainly run internal versions of this. The contribution here is an **open implementation plus a benchmark anyone can rerun**, not a claim to have invented the idea.

## The contract

PushGate exists to enforce one guarantee. Every component has to serve it.

> No pushed artifact reaches more than ring 0 (1% of the fleet) unless:
>
> 1. every consumer version currently deployed loaded it in a sandbox without a crash, a limit breach, or a latency regression over budget, while replaying sampled production traffic;
> 2. its semantic diff from the last known good file stays within budget (row count, cardinality, null rate, distribution shift);
> 3. each ring stays healthy for its bake time.
>
> If any check fails, the push is held, and consumers keep or revert to the last known good file.

## How it works

```
Producer pipeline (e.g. feature file every 5 min)
   │
   ▼
1. Structural check         schema, field counts                      [built: B1]
   ▼
2. Semantic diff            vs last known good: size, cardinality,    [built: B3]
   ▼                        nulls, drift
3. Consumer-in-the-loop     load into each deployed version,          [built: B3]
   ▼                        replay sampled traffic
Ring 0 (1%) ─ healthy after bake ─▶ Ring 1 (10%) ─ healthy after bake ─▶ Ring 2 (100%)   [built: B2]
   │ alarm                              │ alarm
   ▼                                    ▼
        Auto-rollback to last known good                              [built: B2]

Any failure in steps 2 or 3  ─▶  Hold + explain                       [built: B3]
```

**Components**

- **Semantic diff** (built). Compares the candidate with the last known good file: row count, duplicate keys, new rule shapes, new nulls, PSI drift. It holds only on what is almost never legitimate (duplicates, the file doubling) and warns on the rest.
- **Consumer-in-the-loop sandbox** (the core idea, built). The fleet registry lists live consumer versions, say v41 through v44. For each, PushGate starts that release's binary as an isolated process, loads the candidate file, replays about 1,000 sampled requests, and records crashes, panics, latency, memory use, and output differences versus the same release running the last known good file. Output differences are a warning, not a block, since changed behavior is often the point of a push. How both layers work is in [docs/gate.md](docs/gate.md).
- **Consumer-side safety net** (built). A small library in Go (`lib/go/lkg`) and Rust (`lib/rust/lkg`): if a file fails to load, breaks a declared limit or (Go) panics while loading, the consumer keeps serving the last known good file, caches it for restarts, and reports why on `/healthz`. Fail-open vs fail-closed is a per-file setting, because failing open on a security rule file can be the wrong call. Rollout alarms count a refusal as a failure, so a rollout does not carry a file to 100% while consumers quietly reject it. Details in [docs/lkg.md](docs/lkg.md).
- **Ring rollout and kill switch** (built). Rings with bake times, health checks, automatic rollback, and one command to freeze all pushes for a file type. Deliberately simple: AppConfig already does this well.
- **Explanations** (built). Every held push produces a short report, for example: `Held: v41, v42, v43 (200 of 400 instances) crashes on replayed traffic: panicked at src/main.rs:108:20: index out of bounds: the len is 20 but the index is 20 (also 1 warning(s): 5 rules use a 21-field template)`.

## PushBench

A simulated fleet of 500 to 2,000 lightweight consumer processes across a few regions, with mixed versions. Consumers are a small **Rust** proxy module with a preallocated feature table (matching the Cloudflare design) and a **Go** policy checker (matching the Google Service Control design). What differs between releases v41 to v44 is documented in [docs/versions.md](docs/versions.md).

**Strategies compared**

| ID | Strategy | Stands in for | Status |
|---|---|---|---|
| B0 | push-all: validate format, send to 100% | The fast paths in all three incidents | Built |
| B1 | schema: JSON Schema plus field-count checks, push to all | A typical validator | Built |
| B2 | staged + alarms: rings, bake times, alarm rollback | AppConfig-style rollout | Built |
| B3 | PushGate: B2 plus semantic diff and consumer-in-the-loop sandbox, with the last-known-good library in consumers | This project | Built |

How B0 to B2 work, and what B2 cannot see, is in [docs/baselines.md](docs/baselines.md).

**Scenarios**

- **Bad (10):** the three incidents (Cloudflare duplicate rows, CrowdStrike extra input field, Google blank quota), plus a partial-replica Cloudflare variant, growth to 210 features, ordinary growth that only the oldest release cannot hold, a 21st field reached only by a rare request, a single-row Google variant, a half-written file and a wrong-type weight. Full list in [docs/bench.md](docs/bench.md).
- **Good (2 through the fleet, 500 through the gate):** feature and project growth through the fleet, to measure push delay, and a corpus of 500 safe files (added rules, retuned weights, removed or replaced rules, flipped allows, new quotas) to measure false holds. A gate that blocks everything scores perfectly on bad pushes and is useless, so the corpus keeps the results honest.
- **Not built:** a slow memory leak and a rollback storm, which the roadmap planned. Why is in [docs/bench.md](docs/bench.md).

**Metrics**

| Metric | Definition |
|---|---|
| Blast radius | Failed instance-minutes and peak share of fleet failing |
| Caught before exposure | Share of bad pushes held before ring 0 |
| Time to detect and roll back | Push to hold, and hold to fleet-wide last known good |
| False-hold rate | Good pushes held, out of all good pushes |
| Push delay | p50 and p95 time added before 100% rollout |
| Gate cost | CPU-seconds per push for sandbox runs |

The working hypothesis, to be reported whichever way it comes out: B2 limits blast radius to roughly the ring 0 share when ring 0 happens to hold a vulnerable release, while B3 stops most of these shapes before any exposure, at under 2 minutes of push delay and a single-digit false-hold rate.

## Layout

```
pushgate/
├── consumers/
│   ├── rust-proxy/        bot-proxy: feature-table consumer (Cloudflare and CrowdStrike shapes)
│   └── go-policy/         go-policy: quota policy checker (Google shape)
├── fleet/
│   ├── cmd/fleet/         supervisor: runs the fleet, restarts crashes, publishes the registry
│   ├── cmd/pgctl/         status, raw push, wait, crashes
│   └── cmd/loadgen/       steady traffic; per-instance error rates for alarms (-listen)
├── gate/                  B3: semantic diff, consumer-in-the-loop sandbox, held-push reports
│   └── cmd/gate/          gate check: run the gate on one file without delivering it
├── rollout/               B0, B1, B2: validators, rings, bake, alarms, rollback, kill switch
│   ├── schemas/           JSON Schemas for each file format (B1, B2)
│   └── cmd/rollout/       push, set-lkg, rollback, freeze, unfreeze, status
├── bench/
│   ├── scenarios/         incident scenarios (one JSON file each)
│   ├── cmd/pushbench/     runs scenarios x strategies x fleet seeds and measures the damage
│   ├── cmd/goodcorpus/    500 safe files through the schema check and the gate (false holds, gate cost)
│   ├── cmd/report/        tables with confidence intervals, and SVG charts
│   └── results/           committed benchmark results
├── lib/
│   ├── go/lkg/            last-known-good library for Go consumers
│   └── rust/lkg/          the same for Rust consumers
├── internal/registry/     fleet layout, live registry, stable rings
├── producer/              builds feature files from DuckDB, and policy files
├── deploy/                fleet.json (layout), prometheus.yml
├── seed/                  initial config files every instance starts with
├── scripts/               smoke.py (CI check), demo_week1.sh
├── docs/                  versions.md, baselines.md (B0 to B2), gate.md (B3), lkg.md (the library), bench.md (method)
├── .github/workflows/     CI
├── Dockerfile, docker-compose.yml
└── Makefile
```

## Quickstart

Needs Go 1.24+, a recent stable Rust toolchain and Python 3.10+.

```bash
make build        # consumers v41..v44, plus bin/fleet, pgctl, loadgen, rollout, pushbench, gate
make test         # Rust, Go and producer unit tests
make demo-week1   # 500-instance fleet; the Cloudflare-shaped push takes down every bot-proxy, then recovers
make smoke        # all three incident shapes; checks each one breaks exactly the releases it should
make bench        # B0 to B3 on every scenario, about 15 minutes; writes bench/results/latest.md
make bench-lkg    # B0, B2 and B3 with the last-known-good library in every consumer
```

Or run the pieces yourself:

```bash
make fleet                                   # terminal 1: 500 instances, registry on :19000
bin/loadgen -rate 2000                       # terminal 2: traffic, one line per second
bin/pgctl status                             # who runs what, and which config each instance serves
bin/pgctl push -consumer bot-proxy -file f.json -percent 1   # deliver to a stable 1% subset
bin/pgctl crashes                            # crash-looping instances, grouped by panic
bin/pgctl rejections                         # files the last-known-good library refused, grouped by reason

bin/loadgen -rate 2000 -listen 127.0.0.1:19100              # traffic plus per-instance error rates
bin/rollout set-lkg -consumer bot-proxy -file seed/feature.json
bin/rollout -loadgen http://127.0.0.1:19100 push -strategy b2 -bake 10s -consumer bot-proxy -file f.json
bin/loadgen -rate 2000 -listen 127.0.0.1:19100 -record state/traffic.txt   # also keep a traffic sample for the gate
bin/gate check -consumer bot-proxy -file f.json                  # pass or hold, and why; delivers nothing
bin/rollout -loadgen http://127.0.0.1:19100 push -strategy b3 -bake 10s -consumer bot-proxy -file f.json
bin/rollout freeze -consumer bot-proxy -reason "incident 1234"   # kill switch
bin/rollout rollback -consumer bot-proxy                         # last known good, everywhere
```

With Docker, `docker compose up --build -d` runs the fleet plus Prometheus, which scrapes all 500 instances. Prometheus is at <http://localhost:9090> and the registry at <http://localhost:19000/registry>.

### What week 1 shows

`make smoke` on a 4-core machine, 500 instances, 2,000 requests per second, no gate:

| Scenario | Bad file | Crashed |
|---|---|---|
| Cloudflare | Producer query returns every feature twice (110 → 220) | bot-proxy v41, v42, v43, v44: 100% |
| CrowdStrike | 5 rules gain a 21st match field | bot-proxy v41 to v43: 100%. v44: 0% |
| Google Cloud | 3 policy rows with `enforce_quota` and a blank quota | go-policy v43, v44: 100%. v41, v42: 0% |

Each push reached every instance of that consumer in under a second. Rolling back recovered the fleet in about 9 seconds, mostly restart backoff. The Cloudflare file comes from a real SQL flaw: the producer's metadata query filters on table name but not schema, so making a second schema visible doubles its output.

One side effect: with Rust backtraces turned on, 400 crash-looping instances spent enough CPU symbolizing backtraces to stall a 4-core machine. That echoes Cloudflare's finding that error reporting made their outage worse. The supervisor turns backtraces off by default (`-backtraces` turns them back on).

### What week 2 shows

`make bench` on the same machine, 500 instances, 2,000 requests per second, each run watched for 45 s, B2 baking 10 s per ring ([full table](bench/results/week2.md)):

| Incident | B0 push-all | B1 schema | B2 staged + alarms |
|---|---|---|---|
| Cloudflare (bot-proxy, 400) | 400 failing, 100% of requests failed | same as B0 | 4 exposed, alarm at 2 s, rolled back. 0.1% of requests failed |
| CrowdStrike (bot-proxy, 400) | 200 failing, 46% of requests failed | same as B0 | 4 exposed, alarm at 2 s, rolled back. 0.1% failed |
| Google Cloud (go-policy, 100) | 68 failing, 68% of requests failed | same as B0 | 1 exposed, alarm at 2 s, rolled back. 0.2% failed |

Three things stand out:

- **B1's schema caught none of them.** Each bad file is legal in its format. The limits it broke live in consumer code.
- **B2 works well when ring 0 holds a release the file breaks.** At this fleet size, the 1% ring happened to include a vulnerable release in all three cases.
- **It is luck, not design.** On a 100-instance fleet ([table](bench/results/week2-small-fleet.md)), the Google-shaped file reached ring 0 and ring 1 on safe releases only (v41, v42). B2 rolled it out to 100% before the alarm fired, and 60% of that fleet crashed. The CrowdStrike file got past ring 0 the same way. Week 5 measures this across many fleet layouts.

### What week 3 shows

Same machine and settings as week 2, now with B3 and two ablations that run one gate layer each ([full table](bench/results/week3.md), [100-instance fleet](bench/results/week3-small-fleet.md)):

| Incident | B2 staged + alarms | B3 PushGate | B3 diff only | B3 sandbox only |
|---|---|---|---|---|
| Cloudflare | 4 exposed, rolled back | **held, 0 exposed** (diff and sandbox) | held | held |
| CrowdStrike | 4 exposed, rolled back | **held, 0 exposed** (sandbox) | warned, passed: 4 exposed, rolled back | held |
| Google Cloud | 1 exposed, rolled back | **held, 0 exposed** (sandbox) | warned, passed: 1 exposed, rolled back | held |

- **B3 held every bad file before any instance got it,** on the 500- and the 100-instance fleet. On the small fleet B2 again let the Google file reach 100% (12 of 20 go-policy instances crashed) and the CrowdStrike file reach 10%; B3 exposed nothing. Ring 0 luck no longer decides these shapes. Each check took 0.2 to 0.3 s against a one-minute budget.
- **The sandbox does the work.** The diff alone catches only the Cloudflare file. It sees the 21-field rules and the blank quotas, but those are legal changes in general, so it warns rather than holds. Only running v41 to v44 shows which releases crash, which is the project's central claim.
- **The report says what would have happened.** For CrowdStrike: v41, v42 and v43 (200 of 400 instances) crash on the second replayed request with an index out of bounds at `main.rs:108`, while v44 is fine.
- **Good pushes:** week 3 checked only identical files and 10% growth. Week 5 ran 500 safe files through the gate, and none were held.

### What week 4 shows

The same benchmark with every consumer loading through the last-known-good library ([full table](bench/results/week4.md), [100-instance fleet](bench/results/week4-small-fleet.md), [fail-closed](bench/results/week4-fail-closed.md)):

| Incident | B0 push-all, no library | B0 push-all + library | B2 + library | B3 + library |
|---|---|---|---|---|
| Cloudflare | 400 failing, 100% of requests failed | 400 refused the file, 0 failing, 0% failed | 4 refused, rejection alarm at 3 s, rolled back | held by the gate, 0 exposed |
| CrowdStrike | 200 failing, 46% failed | 200 refused (v41 to v43), 0 failing, 0% failed | 2 refused, alarm at 3 s, rolled back | held, 0 exposed |
| Google Cloud | 68 failing, 68% failed | 68 refused (v43, v44), 0 failing, 0% failed | 1 refused, alarm at 2 s, rolled back | held, 0 exposed |

- **The library turns a fleet-wide outage into a non-event.** Pushed to everyone at once, all three files crashed nothing: each vulnerable instance rejected the file with the reason, for example `feature 0 has 21 match fields; this release reads 20 input slots`, and kept serving the old one.
- **Silent rejection is its own risk, so rejections raise the alarm.** Without that, B2 would treat an instance that refuses the file as healthy and carry it to 100%. With it, B2 rolled back at ring 0. On the 100-instance fleet, B2 still reached 100% with the Google file, because rings 0 and 1 again held only safe releases. Nothing failed, but 12 of 20 instances were left on stale config until the alarm fired at 14 s. The gate catches that case before exposure.
- **Fail-closed trades availability for safety, as designed.** With `fail_closed`, B0 failed about as many requests as with no library, since refusing instances answer 503, but nothing crash-looped. B2 recovered in 3 to 6.5 s.
- **Rust panics cannot be caught** (`panic = "abort"`), so bot-proxy relies on declared limits. Go's library also catches panics: go-policy's unchanged v43 code still hits the nil quota, and the library turns that into a rejection naming `main.go:101`.

### What week 5 shows

Every scenario under B0 to B3 on five fleet layouts of 100 instances ([full tables](bench/results/week5.md), [method](docs/bench.md)):

![Share of the fleet that got each bad file](bench/results/week5/exposure.svg)

| Strategy | Bad pushes held before exposure (95% CI) | Mean share of fleet that got a bad file | Mean failed instance-seconds |
|---|---|---|---|
| B0 push-all | 5 of 50, 10% (4 to 21%) | 90% | 778 ± 236 |
| B1 schema | 10 of 50, 20% (11 to 33%) | 80% | 579 ± 213 |
| B2 staged + alarms | 10 of 50, 20% (11 to 33%) | 26% | 13 ± 6 |
| B3 PushGate | 46 of 50, 92% (81 to 97%) | 8% | 0 (3 runs had any failure) |

- **B2's result depends on the layout.** With the same Google file, B2 stopped at 5% of the fleet on two layouts and reached 100% on two others, where rings 0 and 1 held only safe releases. On those, 12 of 20 go-policy instances crashed until the alarm rolled it back, about 10 s later. The same happened to the two v41-only files on one layout in five.
- **B3 removed that variance.** It held every file that the deployed releases fail on with ordinary traffic, on every layout, because it tests each release rather than a sample of instances.
- **The gate is only as good as its traffic sample.** The rare-request file breaks three releases but only on one request shape. The gate held it once in five runs, when the recorded sample happened to contain that request. In the other four it passed, and B3 behaved like B2: the crashes were too rare to trip the alarm, and the file reached the whole fleet.
- **No false holds in 500 safe files** (0%, CI 0 to 0.8%). 28% passed with a warning, mostly a new 21-field shape or changed answers, which is the point of warn over hold. The gate cost 0.13 CPU-seconds and 95 ms per check.
- **The corpus found a gate bug first.** The first run held 9 safe files: parallel sandbox processes raced for the same port, and once a replay hit the wrong process. The fix (own id per run, retry on bind failure) is in this week, and the numbers above are after it.
- **The cheap checks still matter.** B0's format check stopped the half-written file and B1's schema stopped the wrong-type weight. The gate adds to them rather than replacing them.

## Roadmap

| Week | Deliverable |
|---|---|
| 1 ✅ | Fleet simulator and consumers: Rust and Go consumers, 500+ mixed-version processes, version registry, producer, Prometheus metrics |
| 2 ✅ | Baselines B0 to B2: push service, rings, alarm rollback, kill switch. Rebuild the three incidents and confirm B0 takes down the fleet in each |
| 3 ✅ | Gate checks: semantic diff (row count, cardinality, null rate, PSI drift) and sandbox with traffic replay, under 1 minute per run |
| 4 ✅ | Consumer library: last-known-good fallback, declared limits, fail-open/closed setting, held-push reports, randomized reload backoff |
| 5 ✅ | PushBench runs: 12 bad scenarios and 500 good pushes against B0 to B3, 5 seeds, confidence intervals, blast-radius charts |
| **6 ← next** | Write-up and demo: README as a short paper, 90-second video, one-command `make bench` |

## Limitations

- **A simulated fleet is not a real one.** Incidents are rebuilt from the shapes in public postmortems, not the companies' real code or traffic.
- **Results come from one 4-core machine.** Timings such as the 2 s alarm and 9 s recovery depend on that setup. Week 5's seeds vary the fleet layout, not the machine. The seeded matrix runs on the 100-instance fleet to fit in two hours.
- **Replay only catches what the sample covers.** Google's bug sat on a path only one policy shape reached. If the replay misses that request type, so does the gate. In week 5 the gate caught the rare-request scenario in 1 of 5 runs.
- **Speed versus safety is a real tradeoff.** Security content needs to move fast. Results will be reported for both a fast lane and a full gate.
- **Failing open is not always safe.** For auth or security rule files it may let through traffic that should be blocked. The library's `fail_closed` mode exists for those.
- **The library needs a code release.** In the simulation every release gets it through a flag, which assumes a backport. In a real fleet it protects only the releases that ship it.
- **Data-triggered failures only.** PushGate does not catch ordinary code bugs or bad commands such as Facebook's 2021 BGP withdrawal.

## Sources

- [Cloudflare: outage on November 18, 2025](https://blog.cloudflare.com/18-november-2025-outage/)
- [Google Cloud incident report, June 12, 2025](https://status.cloud.google.com/incidents/ow5i3PPK96RduMcb1SsW)
- [CrowdStrike: Channel File 291 root cause analysis](https://crowdstrike.com/wp-content/uploads/2024/08/Executive-Summary_Root-Cause-Analysis_Channel-File-291.pdf)
- [AWS AppConfig: monitoring deployments for automatic rollback](https://docs.aws.amazon.com/appconfig/latest/userguide/monitoring-deployments.html)
- [Codefresh: progressive delivery for ConfigMaps with Argo Rollouts](https://codefresh.io/blog/progressive-delivery-for-kubernetes-config-maps-using-argo-rollouts)
- [Sun et al., Testing Configuration Changes in Context to Prevent Production Failures (OSDI 2020)](https://www.usenix.org/conference/osdi20/presentation/sun)

The 8.5M device figure for CrowdStrike is Microsoft's public estimate from July 2024. Meta's Configerator paper (SOSP 2015) is cited from memory.

## License

To be decided. 
