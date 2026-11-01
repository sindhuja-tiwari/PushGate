# PushGate

**Stop a bad config push before it takes down the fleet.**

PushGate is an open-source safety gate for fast-moving config and data pushes. Before a new file reaches your fleet, PushGate loads it into the *actual consumer binary*, at every version currently deployed, replays sampled traffic, and compares the result against the last known good file. Only then does it roll out in rings, with health gates and automatic rollback.

It ships with **PushBench**, a reproducible benchmark of incidents rebuilt from public postmortems, so you can measure how much of a fleet each rollout strategy would have lost.

> **Status: week 2 of 6 done.** The simulated fleet, both consumers at four releases each, the DuckDB producer, and the three baselines (B0 push-all, B1 schema, B2 staged rollout with alarms and rollback) work end to end, with benchmark results committed. **PushGate's own gate (B3) is not built yet.** See [Roadmap](#roadmap).

## Results so far

Three public incidents, rebuilt as scenarios on a 500-instance fleet:

| Strategy | What happened |
|---|---|
| **B0 push-all** | The bad file reached every instance in under a second. 46% to 100% of requests failed. |
| **B1 schema** | Caught none of the three. Each file is legal in its format, and the limits it broke live in consumer code. |
| **B2 staged + alarms** | Contained all three to 0.1% to 0.2% of requests, **but only because ring 0 happened to contain a vulnerable release.** On a 100-instance fleet, B2 rolled the Google-shaped file out to 100% and 60% of the fleet crashed. |

That last row is the gap PushGate targets. Staged rollout picks ring 0 by instance, not by release, so whether it catches a release-specific bad file is partly luck. Running the file through every deployed consumer version *before* exposure removes the luck. Details in [What week 2 shows](#what-week-2-shows).

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
2. Semantic diff            vs last known good: size, cardinality,    [week 3]
   ▼                        nulls, drift
3. Consumer-in-the-loop     load into each deployed version,          [week 3]
   ▼                        replay sampled traffic
Ring 0 (1%) ─ healthy after bake ─▶ Ring 1 (10%) ─ healthy after bake ─▶ Ring 2 (100%)   [built: B2]
   │ alarm                              │ alarm
   ▼                                    ▼
        Auto-rollback to last known good                              [built: B2]

Any failure in steps 2 or 3  ─▶  Hold + explain                       [weeks 3-4]
```

**Components**

- **Consumer-in-the-loop sandbox** (the core idea, week 3). The fleet registry lists live consumer versions, say v41 through v44. For each, PushGate starts a container, loads the candidate file, replays about 1,000 sampled requests, and records crashes, panics, memory use, and output differences versus the last known good file. Output differences are a warning, not a block, since changed behavior is often the point of a push.
- **Consumer-side safety net** (week 4). A small library: if a file fails to load or breaks a declared limit, the consumer keeps serving with the last known good file and reports why. Fail-open vs fail-closed is a per-file setting, because failing open on a security rule file can be the wrong call.
- **Ring rollout and kill switch** (built). Rings with bake times, health checks, automatic rollback, and one command to freeze all pushes for a file type. Deliberately simple: AppConfig already does this well.
- **Explanations** (weeks 3 to 4). Every held push produces a short report, for example: `Held: feature count 400 > consumer v44 limit 200; v44 panicked at features.rs:118; row count +118% vs last good.`

## PushBench

A simulated fleet of 500 to 2,000 lightweight consumer processes across a few regions, with mixed versions. Consumers are a small **Rust** proxy module with a preallocated feature table (matching the Cloudflare design) and a **Go** policy checker (matching the Google Service Control design). What differs between releases v41 to v44 is documented in [docs/versions.md](docs/versions.md).

**Strategies compared**

| ID | Strategy | Stands in for | Status |
|---|---|---|---|
| B0 | push-all: validate format, send to 100% | The fast paths in all three incidents | Built |
| B1 | schema: JSON Schema plus field-count checks, push to all | A typical validator | Built |
| B2 | staged + alarms: rings, bake times, alarm rollback | AppConfig-style rollout | Built |
| B3 | PushGate: B2 plus semantic diff, consumer-in-the-loop, last-known-good library | This project | Week 3 to 4 |

How B0 to B2 work, and what B2 cannot see, is in [docs/baselines.md](docs/baselines.md).

**Scenarios**

- **Done (3):** Cloudflare shape (duplicate rows past a preallocated limit), CrowdStrike shape (extra input field), Google Cloud shape (blank field on an unflagged path).
- **Planned (about 9 more):** a producer flipping between good and bad output, a file safe for v44 but crashing v41, a slow memory leak that appears after 10 minutes, a failure that needs a rare request type the replay may miss, a rollback storm, and variations on the three above.
- **Good-push corpus (about 500, not built):** added rules, retuned weights, normal 5 to 20% growth. This measures how often the gate wrongly blocks a safe push. A gate that blocks everything scores perfectly on bad pushes and is useless, so this corpus keeps the results honest.

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
├── rollout/               B0, B1, B2: validators, rings, bake, alarms, rollback, kill switch
│   ├── schemas/           JSON Schemas for each file format (B1, B2)
│   └── cmd/rollout/       push, set-lkg, rollback, freeze, unfreeze, status
├── bench/
│   ├── scenarios/         incident scenarios (one JSON file each)
│   ├── cmd/pushbench/     runs scenarios x strategies and measures the damage
│   └── results/           committed benchmark results
├── internal/registry/     fleet layout, live registry, stable rings
├── producer/              builds feature files from DuckDB, and policy files
├── deploy/                fleet.json (layout), prometheus.yml
├── seed/                  initial config files every instance starts with
├── scripts/               smoke.py (CI check), demo_week1.sh
├── docs/                  versions.md (release differences), baselines.md (B0 to B2)
├── .github/workflows/     CI
├── Dockerfile, docker-compose.yml
└── Makefile
```

Planned for later weeks: `gate/` (semantic diff, consumer-in-the-loop sandbox), `lib/` (last-known-good library), and the good-push corpus under `bench/`.

## Quickstart

Needs Go 1.24+, a recent stable Rust toolchain and Python 3.10+.

```bash
make build        # consumers v41..v44, plus bin/fleet, pgctl, loadgen, rollout, pushbench
make test         # Rust, Go and producer unit tests
make demo-week1   # 500-instance fleet; the Cloudflare-shaped push takes down every bot-proxy, then recovers
make smoke        # all three incident shapes; checks each one breaks exactly the releases it should
make bench        # B0, B1 and B2 on every scenario, about 10 minutes; writes bench/results/latest.md
```

Or run the pieces yourself:

```bash
make fleet                                   # terminal 1: 500 instances, registry on :19000
bin/loadgen -rate 2000                       # terminal 2: traffic, one line per second
bin/pgctl status                             # who runs what, and which config each instance serves
bin/pgctl push -consumer bot-proxy -file f.json -percent 1   # deliver to a stable 1% subset
bin/pgctl crashes                            # crash-looping instances, grouped by panic

bin/loadgen -rate 2000 -listen 127.0.0.1:19100              # traffic plus per-instance error rates
bin/rollout set-lkg -consumer bot-proxy -file seed/feature.json
bin/rollout -loadgen http://127.0.0.1:19100 push -strategy b2 -bake 10s -consumer bot-proxy -file f.json
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

## Roadmap

| Week | Deliverable |
|---|---|
| 1 ✅ | Fleet simulator and consumers: Rust and Go consumers, 500+ mixed-version processes, version registry, producer, Prometheus metrics |
| 2 ✅ | Baselines B0 to B2: push service, rings, alarm rollback, kill switch. Rebuild the three incidents and confirm B0 takes down the fleet in each |
| **3 ← next** | Gate checks: semantic diff (row count, cardinality, null rate, PSI drift) and sandbox with traffic replay, under 1 minute per run |
| 4 | Consumer library: last-known-good fallback, declared limits, fail-open/closed setting, held-push reports, randomized reload backoff |
| 5 | PushBench runs: 12 bad scenarios and 500 good pushes against B0 to B3, 5 seeds, confidence intervals, blast-radius charts |
| 6 | Write-up and demo: README as a short paper, 90-second video, one-command `make bench` |

**Smallest useful first step for week 3:** a load-only dry run. Each consumer version loads a candidate file and exits non-zero on a panic or limit breach, and a `gate/` command runs that once per live version from the registry (v41 to v44). No traffic replay or semantic diff yet. That alone would hold all three rebuilt incident files before ring 0, which is the main claim, and the diff and replay layers then add to something already working.

## Limitations

- **A simulated fleet is not a real one.** Incidents are rebuilt from the shapes in public postmortems, not the companies' real code or traffic.
- **Results so far come from one 4-core machine.** Timings such as the 2 s alarm and 9 s recovery depend on that setup. Week 5 adds seeds and confidence intervals.
- **Replay only catches what the sample covers.** Google's bug sat on a path only one policy shape reached. If the replay misses that request type, so does the gate. A rare-request scenario will measure this directly.
- **Speed versus safety is a real tradeoff.** Security content needs to move fast. Results will be reported for both a fast lane and a full gate.
- **Failing open is not always safe.** For auth or security rule files it may let through traffic that should be blocked.
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
