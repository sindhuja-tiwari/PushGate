# PushBench week 7: slow scenarios

Generated 2026-11-07 23:05 UTC on 4 CPUs. Traffic 1000 req/s. Each run watched the fleet for 45s after the push, unless its scenario sets a longer window (memory leak 240 s, flapping producer 100 s). B2 bake time 5s per ring (rings 1%, 10%, 100%).

Consumers: original naive loaders (no last-known-good library).

| Incident | Seed | Strategy | Outcome | Gate | Got the bad file | Refused it | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Cloudflare, flapping producer | 1 | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 4440 | 52.8% | – | 82.5 s |
| Cloudflare, flapping producer | 1 | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 4350 | 52.5% | – | 82.5 s |
| Cloudflare, flapping producer | 1 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 13 | 0.1% | 2.0 s | 65.0 s |
| Cloudflare, flapping producer | 1 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Slow memory leak | 1 | B0 | completed | – | 80 of 80 (100%) | – | 7 (9%) | 52 | 0.0% | – | 158.0 s |
| Slow memory leak | 1 | B1 | completed | – | 80 of 80 (100%) | – | 5 (6%) | 82 | 0.1% | – | 208.5 s |
| Slow memory leak | 1 | B2 | completed | – | 80 of 80 (100%) | – | 5 (6%) | 76 | 0.1% | – | not within 240 s |
| Slow memory leak | 1 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, flapping producer | 2 | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 4434 | 52.8% | – | 82.5 s |
| Cloudflare, flapping producer | 2 | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 4400 | 52.8% | – | 82.5 s |
| Cloudflare, flapping producer | 2 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 18 | 0.2% | 2.0 s | 67.0 s |
| Cloudflare, flapping producer | 2 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Slow memory leak | 2 | B0 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 46 | 0.0% | – | 151.5 s |
| Slow memory leak | 2 | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 87 | 0.0% | – | 216.0 s |
| Slow memory leak | 2 | B2 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 80 | 0.0% | – | not within 240 s |
| Slow memory leak | 2 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, flapping producer | 3 | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 4404 | 52.1% | – | 84.5 s |
| Cloudflare, flapping producer | 3 | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 4453 | 53.2% | – | 82.5 s |
| Cloudflare, flapping producer | 3 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 18 | 0.2% | 2.0 s | 69.0 s |
| Cloudflare, flapping producer | 3 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Slow memory leak | 3 | B0 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 44 | 0.0% | – | 157.5 s |
| Slow memory leak | 3 | B1 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 93 | 0.1% | – | 208.0 s |
| Slow memory leak | 3 | B2 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 86 | 0.0% | – | not within 240 s |
| Slow memory leak | 3 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |

Refused it: the most instances at once whose last-known-good library rejected the bad file. With keep_last_good they keep serving the old file, so they are not counted as failing; with fail_closed they stop answering and are.

Gate: what B3's pre-exposure gate decided, which layer held the file, and how long the gate took. B3-DIFF and B3-SANDBOX run one layer each.

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.

## Memory leak under B2 on a fresh fleet

In the table above, each seed's runs share one fleet, and a v44 process keeps what it leaked after the rollback to the good file until it restarts. So the B1 and B2 memory-leak runs started with v44 instances already partly full, and failed sooner (about 24 s and 63 s after the push) than they would have on fresh instances. B0 ran first on each fleet and is clean. The B2 runs below were repeated alone, each on a freshly started fleet (`bench/results/week7-leak-b2.json`). PushBench now restarts the fleet around every run of a long scenario.

| Incident | Seed | Strategy | Outcome | Gate | Got the bad file | Refused it | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Slow memory leak | 1 | B2 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 57 | 0.0% | – | 165.0 s |
| Slow memory leak | 2 | B2 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 46 | 0.0% | – | 163.0 s |
| Slow memory leak | 3 | B2 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 44 | 0.0% | – | 163.0 s |

The push completed in about 10 s on every layout. The first v44 instance hit the 64 MB limit 136 to 139 s after the push, and each of the 40 v44 instances was killed once in the 4-minute window.
