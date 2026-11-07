# PushBench results

Generated 2026-11-07 23:19 UTC on 4 CPUs. Traffic 1000 req/s. Each run watched the fleet for 45s after the push, unless its scenario sets a longer window. B2 bake time 5s per ring (rings 1%, 10%, 100%).

Consumers: original naive loaders (no last-known-good library).

| Incident | Seed | Strategy | Outcome | Gate | Got the bad file | Refused it | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Slow memory leak | 1 | B2 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 57 | 0.0% | – | 165.0 s |
| Slow memory leak | 2 | B2 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 46 | 0.0% | – | 163.0 s |
| Slow memory leak | 3 | B2 | completed | – | 80 of 80 (100%) | – | 6 (8%) | 44 | 0.0% | – | 163.0 s |

Refused it: the most instances at once whose last-known-good library rejected the bad file. With keep_last_good they keep serving the old file, so they are not counted as failing; with fail_closed they stop answering and are.

Gate: what B3's pre-exposure gate decided, which layer held the file, and how long the gate took. B3-DIFF and B3-SANDBOX run one layer each.

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.
