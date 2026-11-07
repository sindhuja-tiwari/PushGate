# PushBench (week 7)

PushBench pushes bad and good files through each rollout strategy on a live
simulated fleet and measures what the fleet went through. It runs every
scenario under B0 to B3, on five fleet layouts, plus a corpus of 500 safe
files through the checks alone. Week 7 reran it with the gate's targeted
requests and memory check, and added two slow scenarios that run separately.

Results: [bench/results/week7.md](../bench/results/week7.md). Good-push corpus:
[bench/results/week7-good.md](../bench/results/week7-good.md). Slow scenarios:
[bench/results/week7-long.md](../bench/results/week7-long.md). Week 5's
results, before targeted requests, stay in `bench/results/week5*`.

## What is run

```bash
make build
bin/goodcorpus -n 500 -out bench/results/week7-good.json -md bench/results/week7-good.md
bin/pushbench -scale 0.2 -rate 1000 -seeds 1,2,3,4,5 -strategies b0,b1,b2,b3 -mem-limit-mb 64 \
  -window 25s -bake 5s -out bench/results/week7.json -md bench/results/week7-raw.md
bin/report -title "week 7" -runs bench/results/week7.json -good bench/results/week7-good.json \
  -out bench/results/week7 -md bench/results/week7.md
bin/pushbench -scale 0.2 -rate 1000 -seeds 1,2,3 -strategies b0,b1,b2,b3 -mem-limit-mb 64 -long \
  -scenarios memory-leak,flapping-producer -bake 5s -out bench/results/week7-long.json -md bench/results/week7-long.md
```

`make bench` runs the first three, writing to `state/results/`
(`make bench-quick`: one fleet layout and 100 safe files), and
`make bench-long` runs the last. On a 4-core machine the corpus takes under
2 minutes, the matrix about 2 hours and the slow scenarios about an hour.

**Memory limit.** `-mem-limit-mb 64` gives every fleet instance a 64 MB
resident memory limit, like a container limit: the supervisor samples each
process every 500 ms and kills it when it goes over, and the instance then
restarts like after a crash. A healthy consumer stays under 10 MB.

**Fleet.** `-scale 0.2` gives 100 instances: 80 bot-proxy and 20 go-policy,
across the same releases (v41 to v44) and the same release mix as the full
500-instance fleet. The full fleet would need about five times as long for the
same number of runs; weeks 2 to 4 already report it for the three incidents.

**Seeds.** `bin/fleet -seed N` reshuffles which release each instance runs.
Rings are picked by instance, so the seed decides which releases land in
ring 0 and ring 1. This is the main source of variance for B2, so it is what
the seeds vary. Each seed starts a fresh fleet.

**Window.** Each run watches the fleet for 25 s after the push, sampling every
250 ms. A run whose push was held with nothing exposed stops early, since the
fleet cannot change after that.

## Scenarios

Twelve bad scenarios and two good pushes. The first ten and the good pushes
run in `make bench`; the last two need longer windows and run in
`make bench-long`. "Breaks" is the set of releases that
fail on the file, checked by the producer tests and the gate's sandbox tests.

| Scenario | Consumer | Breaks | What is wrong with the file |
|---|---|---|---|
| `cloudflare-duplicate-rows` | bot-proxy | all | every feature listed twice: 220 rows past the 200-feature table |
| `cloudflare-partial-replica` | bot-proxy | v41 | the same bug when only part of the replica is visible: 140 rows, 110 unique, past v41's 128 |
| `feature-explosion` | bot-proxy | all | 210 distinct features |
| `growth-past-v41` | bot-proxy | v41 | ordinary growth to 130 features, fine for every release but the oldest |
| `crowdstrike-extra-field` | bot-proxy | v41 to v43 | five rules with a 21st match field |
| `crowdstrike-rare-request` | bot-proxy | v41 to v43 | one 21-field rule that only a rare request shape reaches |
| `truncated-file` | bot-proxy | all | the producer wrote half the file |
| `wrong-type-weight` | bot-proxy | all | one weight is a string |
| `google-blank-quota` | go-policy | v43, v44 | three enforced policies with a blank quota block |
| `google-single-row` | go-policy | v43, v44 | the same with one policy |
| `good-feature-growth` | bot-proxy | none | 10 new features (120 in total) |
| `good-policy-growth` | go-policy | none | 5 new projects |

| `memory-leak` | bot-proxy | v44 | debug tracing on for a rule that matches every GET; v44 keeps 128 KB per traced match and is killed at the memory limit about two minutes in (window 4 min) |
| `flapping-producer` | bot-proxy | all | the duplicate-row file and the good file pushed in turn every 15 s, six pushes (window 100 s) |

**Memory leak.** v44 keeps a copy of every request that matches a rule with
`"trace": true` and never frees it ([versions.md](versions.md)). Nothing
crashes on load or on any single request, so B2's 5 s bakes see a healthy
fleet and the push completes. Each v44 instance then grows by about 0.5 MB a
second under the benchmark's load until the memory limit kills it, restarts
on the same file, and grows again.

**Flapping producer.** Cloudflare's producer did not fail once: the file
flipped between good and bad every few minutes, depending on which database
nodes ran the query. The scenario pushes bad, good, bad, good, bad, good,
15 s apart, without waiting for the previous rollout. The run's exposure
counts the bad push that reached the most instances, and it also records how
many file deliveries the fleet took in all.

## Good-push corpus

`bin/goodcorpus` builds 500 safe files from the seed files, half feature files
and half policy files, each with one to three routine changes picked at
random (seed 1):

- feature files: add rules (staying at or under v41's 128), remove rules,
  retune weights by up to ±30%, replace rules, edit a country, widen rules
  with a wildcard 21st field (ignored by every release);
- policy files: add or remove projects, flip allow on some policies, change
  quota limits, enforce a quota with a complete quota block.

Every file is within every release's limits, so every hold is a false hold.
Each file goes through B1's schema check and through the gate three times
(diff only, sandbox only, both), against the real consumer binaries for v41 to
v44, the traffic sample in `gate/testdata/traffic.txt` and the targeted
requests the gate builds for every changed rule. The corpus does not
go through the fleet: the gate's verdict is the only thing that differs
between B2 and B3 on a safe file. The two good scenarios in the matrix
measure the push delay that B2 and B3 add on a live fleet.

## Statistics

- **Proportions** (held before exposure, runs with any failure, false holds)
  get a 95% Wilson score interval, which behaves at 0 and at n.
- **Means over seeds** (exposure, failed instance-seconds, failed requests)
  get a 95% Student t interval with n−1 degrees of freedom (t = 2.776 for
  five seeds), shown as mean ± half-width.
- A run counts as **held** when no instance received the file.

## Charts

`bin/report` writes two static SVGs into `bench/results/week7/`:

- `exposure.svg`: share of the fleet that got each bad file, per strategy,
  mean over seeds with the range as a whisker.
- `timeline.svg`: failing instances over time for the scenario and seed where
  B2 did worst, with B0 and B3 on the same axes.

## A bug the corpus found

The first corpus run held 9 of the 500 safe files, all with `address already
in use`. The sandbox picked a free port, closed it and passed it to the
consumer, and with eight sandboxed processes starting at once another one
sometimes took the port in between. In one case the health check even reached
the other process, so a replay was charged to the wrong release. The sandbox
now gives each run its own id, accepts `/healthz` only when it answers with
that id, and retries on a new port when binding fails. The corpus results are
from after the fix.
