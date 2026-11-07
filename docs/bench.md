# PushBench (week 5)

PushBench pushes bad and good files through each rollout strategy on a live
simulated fleet and measures what the fleet went through. Week 5 runs the full
matrix: every scenario under B0 to B3, on five fleet layouts, plus a corpus of
500 safe files through the checks alone.

Results: [bench/results/week5.md](../bench/results/week5.md). Good-push corpus:
[bench/results/week5-good.md](../bench/results/week5-good.md).

## What is run

```bash
make build
bin/goodcorpus -n 500 -out bench/results/week5-good.json -md bench/results/week5-good.md
bin/pushbench -scale 0.2 -rate 1000 -seeds 1,2,3,4,5 -strategies b0,b1,b2,b3 \
  -window 25s -bake 5s -out bench/results/week5.json -md state/bench/week5-raw.md
bin/report        # tables and charts: bench/results/week5.md, bench/results/week5/*.svg
```

`make bench-week5` runs all three. On a 4-core machine the corpus takes under
2 minutes and the matrix about 2 hours.

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

Ten bad scenarios and two good pushes. "Breaks" is the set of releases that
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

The roadmap planned 12 bad scenarios. Two are not built:

- **A slow memory leak.** It needs a consumer change that leaks per request
  and a window of ten minutes or more per run, which would multiply the
  matrix time by about 20. The gate is not expected to catch it (see
  [gate.md](gate.md), "What it cannot see"), so the rings would carry it.
- **A rollback storm.** Repeated bad pushes while a rollback is in progress
  need a scripted producer in the loop, not a single file per run.

Both remain good candidates for a follow-up.

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
v44 and the traffic sample in `gate/testdata/traffic.txt`. The corpus does not
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

`bin/report` writes two static SVGs into `bench/results/week5/`:

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
