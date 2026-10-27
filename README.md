# PushGate

**Stop a bad config push before it takes down the fleet.**

PushGate is an open-source safety gate for fast-moving config and data pushes. Before a new file reaches your fleet, PushGate loads it into the *actual consumer binary*, at every version currently deployed, replays sampled traffic, and compares the result against the last known good file. Only then does it roll out in rings, with health gates and automatic rollback.

It ships with **PushBench**, a reproducible benchmark of incidents rebuilt from public postmortems, so you can measure how much of a fleet each rollout strategy would have lost.

> **Status: design phase.** This repository currently holds the plan, not the implementation. See [Roadmap](#roadmap) for what gets built and in what order.

---

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
1. Structural check         schema, field counts
   ▼
2. Semantic diff            vs last known good: size, cardinality, nulls, drift
   ▼
3. Consumer-in-the-loop     load into each deployed version, replay sampled traffic
   ▼
Ring 0 (1%) ─ healthy after bake ─▶ Ring 1 (10%) ─ healthy after bake ─▶ Ring 2 (100%)
   │ alarm                              │ alarm
   ▼                                    ▼
        Auto-rollback to last known good

Any failure in steps 2 or 3  ─▶  Hold + explain
```

**Components**

- **Consumer-in-the-loop sandbox** (the core idea). The fleet registry lists live consumer versions, say v41 through v44. For each, PushGate starts a container, loads the candidate file, replays about 1,000 sampled requests, and records crashes, panics, memory use, and output differences versus the last known good file. Output differences are a warning, not a block, since changed behavior is often the point of a push.
- **Consumer-side safety net.** A small library: if a file fails to load or breaks a declared limit, the consumer keeps serving with the last known good file and reports why. Fail-open vs fail-closed is a per-file setting, because failing open on a security rule file can be the wrong call.
- **Ring rollout and kill switch.** Rings with bake times, health checks (error rate, crash loops, p99), automatic rollback, and one command to freeze all pushes for a file type. Deliberately simple: AppConfig already does this well.
- **Explanations.** Every held push produces a short report, for example:
  `Held: feature count 400 > consumer v44 limit 200; v44 panicked at features.rs:118; row count +118% vs last good.`

## PushBench

A simulated fleet of 500 to 2,000 lightweight consumer processes across a few regions, with mixed versions. Consumers are a small **Rust** proxy module with a preallocated feature table (matching the Cloudflare design) and a **Go** policy checker (matching the Google Service Control design).

**Strategies compared**

| ID | Strategy | Stands in for |
|---|---|---|
| B0 | push-all: validate format, send to 100% | The fast paths in all three incidents |
| B1 | schema: JSON Schema plus field-count checks, push to all | A typical validator |
| B2 | staged + alarms: rings, bake times, alarm rollback | AppConfig-style rollout |
| B3 | PushGate: B2 plus semantic diff, consumer-in-the-loop, last-known-good library | This project |

**Scenarios**

- About 12 bad pushes: extra input field, null field on an unflagged path, duplicate rows past a preallocated limit, a producer flipping between good and bad output, a file safe for v44 but crashing v41, a slow memory leak that appears after 10 minutes, a failure that needs a rare request type the replay may miss, and a rollback storm.
- About 500 good pushes (added rules, retuned weights, normal 5 to 20% growth) to measure how often the gate wrongly blocks a safe push.

**Metrics**

| Metric | Definition |
|---|---|
| Blast radius | Failed instance-minutes and peak share of fleet failing |
| Caught before exposure | Share of bad pushes held before ring 0 |
| Time to detect and roll back | Push to hold, and hold to fleet-wide last known good |
| False-hold rate | Good pushes held, out of all good pushes |
| Push delay | p50 and p95 time added before 100% rollout |
| Gate cost | CPU-seconds per push for sandbox runs |

The working hypothesis, to be reported whichever way it comes out: B2 limits blast radius to roughly the ring 0 share, while B3 stops most of these shapes before any exposure, at under 2 minutes of push delay and a single-digit false-hold rate.

## Planned layout

```
pushgate/
├── consumers/
│   ├── rust-proxy/        feature-table consumer (Cloudflare shape)
│   └── go-policy/         policy checker (Google shape)
├── producer/              writes a feature file every N minutes from DuckDB
├── gate/                  structural check, semantic diff, sandbox runner
├── rollout/               rings, bake times, rollback, kill switch
├── lib/                   last-known-good fallback library for consumers
├── bench/
│   ├── scenarios/         bad-push scenarios (about 12)
│   ├── corpus/            good-push corpus (about 500)
│   └── run.py             runs B0 to B3, 5 seeds each
├── docker-compose.yml     fleet simulator
└── Makefile               make demo, make bench
```

## Quickstart (planned)

```bash
make demo    # replay a Cloudflare-shaped push against B0, B2 and B3
make bench   # run the full PushBench suite and produce charts
```

Neither target exists yet. See the roadmap.

## Roadmap

| Week | Deliverable |
|---|---|
| 1 | Fleet simulator and consumers: Rust and Go consumers, 500+ mixed-version processes, version registry, producer, Prometheus metrics |
| 2 | Baselines B0 to B2: push service, rings, alarm rollback, kill switch. Rebuild the three incidents and confirm B0 takes down the fleet in each |
| 3 | Gate checks: semantic diff (row count, cardinality, null rate, PSI drift) and sandbox with traffic replay, under 1 minute per run |
| 4 | Consumer library: last-known-good fallback, declared limits, fail-open/closed setting, held-push reports, randomized reload backoff |
| 5 | PushBench runs: 12 bad scenarios and 500 good pushes against B0 to B3, 5 seeds, confidence intervals, blast-radius charts |
| 6 | Write-up and demo: README as a short paper, 90-second video, one-command `make bench` |

## Limitations

- **A simulated fleet is not a real one.** Incidents are rebuilt from the shapes in public postmortems, not the companies' real code or traffic.
- **Replay only catches what the sample covers.** Google's bug sat on a path only one policy shape reached. If the replay misses that request type, so does the gate. The rare-request scenario measures this directly.
- **Speed versus safety is a real tradeoff.** Security content needs to move fast. Results are reported for both a fast lane and a full gate.
- **Failing open is not always safe.** For auth or security rule files it may let through traffic that should be blocked.
- **Data-triggered failures only.** PushGate does not catch ordinary code bugs or bad commands such as Facebook's 2021 BGP withdrawal.

## Sources

- [Cloudflare: outage on November 18, 2025](https://blog.cloudflare.com/18-november-2025-outage/)
- [Google Cloud incident report, June 12, 2025](https://status.cloud.google.com/incidents/ow5i3PPK96RduMcb1SsW)
- [CrowdStrike: Channel File 291 root cause analysis](https://crowdstrike.com/wp-content/uploads/2024/08/Executive-Summary_Root-Cause-Analysis_Channel-File-291.pdf)
- [AWS AppConfig: monitoring deployments for automatic rollback](https://docs.aws.amazon.com/appconfig/latest/userguide/monitoring-deployments.html)
- [Codefresh: progressive delivery for ConfigMaps with Argo Rollouts](https://codefresh.io/blog/progressive-delivery-for-kubernetes-config-maps-using-argo-rollouts)
- [Sun et al., Testing Configuration Changes in Context to Prevent Production Failures (OSDI 2020)](https://www.usenix.org/conference/osdi20/presentation/sun)

The 8.5M device figure for CrowdStrike is Microsoft's public estimate from July 2024. Meta's Configerator paper (SOSP 2015) is cited from memory and has not been re-checked.

## License

To be decided (MIT or Apache-2.0 suggested).
