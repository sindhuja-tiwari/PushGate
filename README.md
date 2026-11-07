# PushGate

**Stop a bad config push before it takes down the fleet.**

[![The demo: B2 carries a bad policy file to the whole fleet, PushGate holds it](docs/demo-preview.gif)](docs/demo.mp4)

*90-second demo ([docs/demo.mp4](docs/demo.mp4)): a live run on a 100-instance fleet. The clip above is from act 2: staged rollout carries a Google-shaped file to every instance and 12 of 20 crash, then PushGate holds the same file before any instance gets it.*

## Abstract

Three of the largest outages of 2024 and 2025 (CrowdStrike, Google Cloud, Cloudflare) followed one pattern. A data or config file passed its format checks, crashed the program that loaded it, and reached every machine faster than a code release would have been allowed to. PushGate is an open-source gate for such pushes. Before any instance gets a new file, it loads the file into the actual consumer binary at every release currently deployed, replays sampled traffic, and compares the result with the last known good file. Only then does it roll out in rings with alarms and rollback. We evaluate it with PushBench, a benchmark of incidents rebuilt from public postmortems, on a live simulated fleet. Across 10 bad files and 5 fleet layouts, PushGate held 46 of 50 bad pushes before any instance got the file (92%, 95% CI 81 to 97%). Staged rollout with alarms held 10 of 50. PushGate held none of 500 safe files (0%, CI 0 to 0.8%) and cost 0.1 to 0.3 s and about 0.13 CPU-seconds per check. All four misses are one scenario: a crash that only a rare request triggers, which the gate catches only when its traffic sample happens to include that request.

## 1. The problem

| Incident | The file | Why checks missed it | Impact |
|---|---|---|---|
| CrowdStrike, 19 Jul 2024 | Channel File 291 | Sensor expected 20 fields, got 21: out-of-bounds read | ~8.5M Windows machines crashed |
| Google Cloud, 12 Jun 2025 | Quota policy row with blank fields | Code had no null handling and no feature flag | Global API failures, ~7 h to full recovery |
| Cloudflare, 18 Nov 2025 | Bot Management feature file | Duplicate rows doubled the file past a 200-feature preallocation | Widespread 5xx errors, ~6 h to recovery |

In every case the file was well formed, and the failure appeared only when the **real consumer** loaded it. Schema validation would have caught none of them. Staged rollout with health alarms (AWS AppConfig, Argo Rollouts, Flagger) helps, but it picks its first ring by instance, not by release. A file that breaks only some releases is caught early only if those releases happen to land in ring 0.

| Tool | Checks | When |
|---|---|---|
| JSON Schema and validators | Shape | Before rollout |
| AWS AppConfig, Argo Rollouts, Flagger | Health (alarms) | After exposure |
| ctest (OSDI 2020) | Hand-edited config values, via unit tests | Before rollout, Java systems |
| **PushGate** | **Behavior, in the real consumer, at every live release** | **Before exposure** |

Large companies almost certainly run internal versions of this. The contribution here is an **open implementation plus a benchmark anyone can rerun**, not a claim to have invented the idea.

## 2. Design

PushGate enforces one guarantee:

> No pushed file reaches more than ring 0 (1% of the fleet) unless:
>
> 1. every consumer release currently deployed loaded it in a sandbox without a crash, a limit breach, or a latency regression over budget, while replaying sampled production traffic;
> 2. its semantic diff from the last known good file stays within budget (row count, cardinality, null rate, distribution shift);
> 3. each ring stays healthy for its bake time.
>
> If any check fails, the push is held, and consumers keep or revert to the last known good file.

```
Producer pipeline (e.g. a feature file every 5 min)
   │
   ▼
1. Structural check         schema, field counts                        B1
2. Semantic diff            vs last known good: size, duplicates,       B3
                            nulls, new shapes, PSI drift
3. Consumer-in-the-loop     each deployed release loads the file,       B3
                            replays sampled traffic
   ▼
Ring 0 (1%) ─ bake ─▶ Ring 1 (10%) ─ bake ─▶ Ring 2 (100%)              B2
   │ alarm (crash, error rate, refused file)
   ▼
Rollback to last known good

Hold in step 2 or 3 ─▶ report: which releases fail, how, and how much of the fleet that is
```

**Semantic diff** ([docs/gate.md](docs/gate.md)). Compares the candidate with the last known good file. It holds only on what is almost never legitimate (duplicate keys, the file doubling) and warns on what sometimes is (a new 21-field rule, a blank quota, drift), because only running the consumer shows whether those are fatal.

**Consumer-in-the-loop sandbox** (the core idea). The fleet registry lists the releases running now, here v41 to v44. For each, PushGate starts that exact binary twice, with the candidate and with the last known good file, as isolated processes on private ports. It replays up to 1,000 recorded requests and compares crashes, error share, p99 latency, memory and answers. A crash, more than 1% errors or a large latency regression holds the push. Changed answers only warn, since changed behavior is often the point of a push. A held push reads like this:

```
Held: v43, v44 (12 of 20 instances) crashes loading the file: panic: runtime error: invalid memory
address or nil pointer dereference at main.go:101 (also 1 warning(s): 3 policies enforce quota with a
blank quota block; last known good had 0)
```

**Consumer-side safety net** ([docs/lkg.md](docs/lkg.md)). A small library in Go and Rust. If a file fails to load, breaks a declared limit or (in Go) panics while loading, the consumer keeps serving the last known good file, caches it for restarts and reports the reason on `/healthz`. Fail-open or fail-closed is a per-file setting. Rollout alarms count a refusal as a failure, so a rollout cannot carry a file to 100% while consumers quietly reject it.

**Rings and kill switch** ([docs/baselines.md](docs/baselines.md)). Rings of 1%, 10% and 100% with bake times, crash and error-rate alarms, rollback, and one command to freeze all pushes of a file type. Deliberately simple: AppConfig already does this well.

## 3. Evaluation setup

**Fleet.** A simulated fleet of real processes, 500 at full size, across four regions and four releases (v41 to v44). bot-proxy is a Rust module with a preallocated feature table, matching Cloudflare's design. go-policy is a Go quota checker, matching Google's Service Control. What differs between releases is in [docs/versions.md](docs/versions.md): for example, v41 holds 128 features and v42 to v44 hold 200, and v43 and v44 read the quota block without a nil check. A load generator sends steady traffic and records a sample for the gate to replay.

**Strategies.** B0 pushes to everyone after a format check, like the fast paths in all three incidents. B1 adds a JSON Schema. B2 is a staged rollout with alarms and rollback. B3 is PushGate: B2 plus the gate.

**Scenarios** ([docs/bench.md](docs/bench.md)). Ten bad files: the three incidents, a partial-replica Cloudflare variant, growth to 210 features, ordinary growth to 130 that only v41 cannot hold, a 21st field reached only by a rare request, a single-row Google variant, a half-written file and a wrong-type weight. Two good pushes through the fleet measure push delay. A corpus of 500 safe files (added, removed or retuned rules, flipped allows, new quotas) goes through the gate alone to measure false holds. A gate that holds everything scores perfectly on bad pushes and is useless, so the corpus keeps the result honest.

**Method.** Every scenario runs under every strategy on five fleet layouts. The layout seed decides which release each instance runs, and so which releases land in ring 0. The matrix uses a 100-instance fleet (80 bot-proxy, 20 go-policy) so that 240 runs fit in two hours on 4 cores. Each run watches the fleet for 25 s, with B2 baking 5 s per ring. Proportions get Wilson intervals and means over seeds get Student t intervals, both at 95%. A push counts as held when no instance received the file.

## 4. Results

Full tables: [bench/results/week5.md](bench/results/week5.md). Corpus: [bench/results/week5-good.md](bench/results/week5-good.md).

| Strategy | Bad pushes held before exposure (95% CI) | Mean share of fleet that got a bad file | Mean failed instance-seconds per push |
|---|---|---|---|
| B0 push-all | 5 of 50, 10% (4 to 21%) | 90% | 778 ± 236 |
| B1 schema | 10 of 50, 20% (11 to 33%) | 80% | 579 ± 213 |
| B2 staged + alarms | 10 of 50, 20% (11 to 33%) | 26% | 13 ± 6 |
| **B3 PushGate** | **46 of 50, 92% (81 to 97%)** | **8%** | **0** (3 runs had any failure) |

![Share of the fleet that got each bad file](bench/results/week5/exposure.svg)

**4.1 Staged rollout depends on the layout.** With the same Google file, B2 stopped at 5% of the fleet on two layouts and reached 100% on two others, where rings 0 and 1 held only v41 and v42. There, 12 of 20 go-policy instances crashed until the alarm rolled the file back. The two files that break only v41 reached the whole fleet on one layout in five. B2 still keeps damage low on average (13 failed instance-seconds per push against 778 for B0), because its alarm fires within seconds of exposure (median 7 s after the push). But how far a file gets is decided by where releases sit.

![Failing go-policy instances after the Google-shaped push, on the layout where B2 did worst](bench/results/week5/timeline.svg)

**4.2 The gate removes that variance.** B3 held every file that a deployed release fails on with ordinary traffic, on every layout, because it tests each release rather than a sample of instances. The ablations in the [development log](docs/history.md#what-week-3-shows) show which layer does the work. The diff alone holds only the Cloudflare file, which doubles and repeats every key. The 21-field rules and the blank quotas are legal changes in general, so the diff warns on them, and only running the releases shows which ones crash.

**4.3 What the gate misses.** The rare-request file breaks three releases but only on one request shape. The gate held it in 1 of 5 runs, when the recorded traffic sample happened to contain that request. In the other four it passed, and B3 behaved like B2: the crashes were too rare to trip the alarm, and the file reached the whole fleet. A sampled replay is only as good as its sample.

**4.4 False holds and cost.** None of the 500 safe files was held by B1, by either gate layer alone, or by both (0%, CI 0 to 0.8%). 28% passed with a warning, mostly a new rule shape or changed answers. A check took 95 ms at p50 and 0.13 CPU-seconds with 8 sandboxed processes, or about 0.25 s on a machine also running the fleet. B3 added 0.3 s to a 10 s staged rollout for the good pushes, far under the 2-minute budget we set. The first corpus run did hold 9 safe files, which exposed a race in the gate itself: parallel sandbox processes competed for the same port. The fix (an id per run that `/healthz` must echo, and a retry on bind failure) came before the numbers above.

**4.5 The cheap checks still matter.** B0's format check stopped the half-written file and B1's schema stopped the wrong-type weight. Those are B0's 5 and B1's 10 holds. The gate adds to them rather than replacing them.

**4.6 The safety net works without the gate** ([week 4 results](docs/history.md#what-week-4-shows)). With the last-known-good library in every consumer, even B0 pushing the three incident files to all 500 instances crashed nothing. Each vulnerable instance refused the file with the reason, for example `feature 0 has 21 match fields; this release reads 20 input slots`, and kept serving the old one. The library needs a code release to reach every consumer, though. The gate protects the releases already running.

## 5. Limitations and threats to validity

- **A simulated fleet is not a real one.** Incidents are rebuilt from the shapes in public postmortems, not the companies' code or traffic. The consumers are small, so the sandbox is cheap. A large binary that takes seconds to start would raise the gate's cost.
- **One machine.** Timings such as the 2 s alarm and the 0.1 s check come from one 4-core machine. The seeds vary the fleet layout, not the hardware. The seeded matrix runs on 100 instances, and the three incidents were also run on 500 ([development log](docs/history.md)).
- **The scenarios were written by the same people as the gate.** Eight of ten bad files break at load time, where the sandbox is strongest. The rare-request scenario shows the main blind spot. Two planned scenarios, a slow memory leak and a rollback storm, were not built ([docs/bench.md](docs/bench.md)). The gate is not expected to catch a slow leak, and the rings and alarms remain for that.
- **The safe corpus is synthetic.** Its mutations are routine by construction. Real producers make stranger safe changes, so 0 of 500 is an optimistic bound.
- **Speed versus safety.** Security content has to move fast. At about 0.1 s per check the gate adds little delay here, but a fast lane for urgent content was not built.
- **Failing open is not always safe.** For auth or security rule files it may let through traffic that should be blocked. The library's `fail_closed` mode exists for those.
- **Data-triggered failures only.** PushGate does not catch ordinary code bugs or bad commands such as Facebook's 2021 BGP withdrawal.

## 6. Related work

Staged rollout with automatic rollback is standard: AWS AppConfig monitors alarms during a deployment, and Argo Rollouts and Flagger do the same for Kubernetes config. All of them judge a file after exposure. Testing configuration changes against the code that reads them was studied by Sun et al. (ctest, OSDI 2020) for hand-edited values in Java systems, through existing unit tests. Meta's Configerator (SOSP 2015) combines validators, canaries and automated testing for config changes. PushGate applies the same idea to machine-generated data files, runs the deployed binaries rather than unit tests, and measures the result against staged rollout on incidents anyone can rerun.

## 7. Reproduce it

Needs Go 1.24+, a recent stable Rust toolchain, Python 3.10+ and, for the video, ffmpeg.

```bash
make bench        # the full benchmark: corpus, 240 fleet runs, tables and charts (about 2 hours)
make bench-quick  # one fleet layout and 100 safe files (about 25 minutes)
make demo         # the demo run, live in the terminal (about 1 minute)
make demo-video   # ...and render it to docs/demo.mp4
```

`make bench` writes to `state/results/`, so you can compare with the committed `bench/results/`. `make bench RESULTS=bench/results` replaces them.

Other targets: `make build`, `make test`, `make smoke` (each incident breaks exactly the releases it should), `make bench-incidents` and `make bench-lkg` (the three incidents on 500 instances, without and with the library). The pieces also run on their own:

```bash
make fleet                                   # terminal 1: 500 instances, registry on :19000
bin/loadgen -rate 2000 -listen 127.0.0.1:19100 -record state/traffic.txt   # terminal 2: traffic and a sample
bin/pgctl status                             # who runs what, and which config each instance serves
bin/rollout set-lkg -consumer bot-proxy -file seed/feature.json
bin/gate check -consumer bot-proxy -file f.json                  # pass or hold, and why; delivers nothing
bin/rollout -loadgen http://127.0.0.1:19100 push -strategy b3 -bake 10s -consumer bot-proxy -file f.json
bin/pgctl crashes                            # crash-looping instances, grouped by panic
bin/rollout freeze -consumer bot-proxy -reason "incident 1234"   # kill switch
bin/rollout rollback -consumer bot-proxy                         # last known good, everywhere
```

With Docker, `docker compose up --build -d` runs the fleet plus Prometheus, which scrapes all 500 instances (<http://localhost:9090>, registry at <http://localhost:19000/registry>).

## Layout

```
pushgate/
├── consumers/             bot-proxy (Rust) and go-policy (Go), built as releases v41 to v44
├── fleet/cmd/             fleet supervisor, pgctl, loadgen
├── gate/                  semantic diff, consumer-in-the-loop sandbox, held-push reports; cmd/gate
├── rollout/               B0 to B3: validators, rings, bake, alarms, rollback, kill switch; cmd/rollout
├── lib/                   last-known-good library: go/lkg and rust/lkg
├── bench/
│   ├── scenarios/         one JSON file per scenario
│   ├── cmd/               pushbench (fleet runs), goodcorpus (safe files), report (tables and charts)
│   └── results/           committed results
├── internal/registry/     fleet layout, live registry, stable rings
├── producer/              builds feature files from DuckDB, and policy files
├── scripts/               smoke test, demo and its video renderer
├── docs/                  design notes, method, development log, demo video
├── deploy/, seed/         fleet layout and the files every instance starts with
└── Makefile
```

## Roadmap

| Week | Deliverable |
|---|---|
| 1 ✅ | Fleet simulator and consumers: Rust and Go consumers, 500+ mixed-version processes, version registry, producer, Prometheus metrics |
| 2 ✅ | Baselines B0 to B2: push service, rings, alarm rollback, kill switch. Rebuild the three incidents and confirm B0 takes down the fleet in each |
| 3 ✅ | Gate checks: semantic diff and a sandbox with traffic replay, under 1 minute per run |
| 4 ✅ | Consumer library: last-known-good fallback, declared limits, fail-open or fail-closed, randomized reload backoff |
| 5 ✅ | PushBench runs: 10 bad scenarios (12 planned) and 500 good pushes against B0 to B3, 5 seeds, confidence intervals, charts |
| 6 ✅ | Write-up and demo: this README, a 90-second video, one-command `make bench` |

What each week found at the time is in [docs/history.md](docs/history.md).

## Sources

- [Cloudflare: outage on November 18, 2025](https://blog.cloudflare.com/18-november-2025-outage/)
- [Google Cloud incident report, June 12, 2025](https://status.cloud.google.com/incidents/ow5i3PPK96RduMcb1SsW)
- [CrowdStrike: Channel File 291 root cause analysis](https://crowdstrike.com/wp-content/uploads/2024/08/Executive-Summary_Root-Cause-Analysis_Channel-File-291.pdf)
- [AWS AppConfig: monitoring deployments for automatic rollback](https://docs.aws.amazon.com/appconfig/latest/userguide/monitoring-deployments.html)
- [Codefresh: progressive delivery for ConfigMaps with Argo Rollouts](https://codefresh.io/blog/progressive-delivery-for-kubernetes-config-maps-using-argo-rollouts)
- [Sun et al., Testing Configuration Changes in Context to Prevent Production Failures (OSDI 2020)](https://www.usenix.org/conference/osdi20/presentation/sun)

The 8.5M device figure for CrowdStrike is Microsoft's public estimate from July 2024. Meta's Configerator paper (Tang et al., SOSP 2015) is cited from memory.

## License

To be decided.
