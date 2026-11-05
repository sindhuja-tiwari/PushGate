# PushBench results

Generated 2026-11-04 09:28 UTC on 4 CPUs. Traffic 500 req/s. Each run watched the fleet for 20s after the push. B2 bake time 5s per ring (rings 1%, 10%, 100%).

Consumers: last-known-good library on, mode keep_last_good.

| Incident | Strategy | Outcome | Gate | Got the bad file | Refused it | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|---|---|
| Cloudflare, 18 Nov 2025 | B0 | completed | – | 80 of 80 (100%) | 80 | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | – | 1 of 80 (1%) | 1 | 0 (0%) | 0 | 0.0% | 2.0 s | never failed |
| Cloudflare, 18 Nov 2025 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | 0 | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B0 | completed | – | 80 of 80 (100%) | 40 | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | – | 8 of 80 (10%) | 3 | 0 (0%) | 0 | 0.0% | 8.0 s | never failed |
| CrowdStrike, 19 Jul 2024 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | 0 | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B0 | completed | – | 20 of 20 (100%) | 12 | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | – | 20 of 20 (100%) | 12 | 0 (0%) | 0 | 0.0% | 14.0 s | never failed |
| Google Cloud, 12 Jun 2025 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | 0 | 0 (0%) | 0 | 0.0% | – | never failed |

Refused it: the most instances at once whose last-known-good library rejected the bad file. With keep_last_good they keep serving the old file, so they are not counted as failing; with fail_closed they stop answering and are.

Gate: what B3's pre-exposure gate decided, which layer held the file, and how long the gate took. B3-DIFF and B3-SANDBOX run one layer each.

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.
