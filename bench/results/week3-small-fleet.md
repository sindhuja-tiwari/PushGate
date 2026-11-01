# PushBench results

Generated 2026-11-01 19:38 UTC on 4 CPUs. Traffic 500 req/s. Each run watched the fleet for 20s after the push. B2 bake time 5s per ring (rings 1%, 10%, 100%).

| Incident | Strategy | Outcome | Gate | Got the bad file | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|---|
| Cloudflare, 18 Nov 2025 | B0 | completed | – | 80 of 80 (100%) | 80 (100%) | 1588 | 99.9% | – | not within 20s |
| Cloudflare, 18 Nov 2025 | B1 | completed | – | 80 of 80 (100%) | 80 (100%) | 1600 | 100.0% | – | not within 20s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | – | 1 of 80 (1%) | 1 (1%) | 6 | 0.4% | 2.0 s | 6.0 s |
| Cloudflare, 18 Nov 2025 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B3-DIFF | rejected before delivery | held by diff, 0.0 s | 0 of 80 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B3-SANDBOX | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B0 | completed | – | 80 of 80 (100%) | 40 (50%) | 761 | 46.1% | – | not within 20s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | – | 80 of 80 (100%) | 40 (50%) | 773 | 47.1% | – | not within 20s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | – | 8 of 80 (10%) | 3 (4%) | 17 | 0.9% | 7.0 s | 14.0 s |
| CrowdStrike, 19 Jul 2024 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B3-DIFF | rolled_back | passed, 0.0 s | 8 of 80 (10%) | 3 (4%) | 10 | 0.5% | 7.0 s | 10.0 s |
| CrowdStrike, 19 Jul 2024 | B3-SANDBOX | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B0 | completed | – | 20 of 20 (100%) | 12 (60%) | 240 | 59.0% | – | not within 20s |
| Google Cloud, 12 Jun 2025 | B1 | completed | – | 20 of 20 (100%) | 12 (60%) | 240 | 60.8% | – | not within 20s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | – | 20 of 20 (100%) | 12 (60%) | 72 | 15.6% | 12.0 s | 20.0 s |
| Google Cloud, 12 Jun 2025 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 20 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B3-DIFF | rolled_back | passed, 0.0 s | 20 of 20 (100%) | 12 (60%) | 50 | 9.9% | 12.0 s | 16.0 s |
| Google Cloud, 12 Jun 2025 | B3-SANDBOX | rejected before delivery | held by sandbox, 0.2 s | 0 of 20 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |

Gate: what B3's pre-exposure gate decided, which layer held the file, and how long the gate took. B3-DIFF and B3-SANDBOX run one layer each.

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.
