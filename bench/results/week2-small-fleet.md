# PushBench results

Generated 2026-10-31 16:44 UTC on 4 CPUs. Traffic 500 req/s. Each run watched the fleet for 20s after the push. B2 bake time 5s per ring (rings 1%, 10%, 100%).

| Incident | Strategy | Outcome | Got the bad file | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|
| Cloudflare, 18 Nov 2025 | B0 | completed | 80 of 80 (100%) | 80 (100%) | 1600 | 100.0% | – | not within 20s |
| Cloudflare, 18 Nov 2025 | B1 | completed | 80 of 80 (100%) | 80 (100%) | 1599 | 99.7% | – | not within 20s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | 1 of 80 (1%) | 1 (1%) | 6 | 0.3% | 2.0 s | 6.0 s |
| CrowdStrike, 19 Jul 2024 | B0 | completed | 80 of 80 (100%) | 40 (50%) | 754 | 45.8% | – | not within 20s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | 80 of 80 (100%) | 40 (50%) | 762 | 48.2% | – | not within 20s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | 8 of 80 (10%) | 3 (4%) | 14 | 0.7% | 7.0 s | 12.0 s |
| Google Cloud, 12 Jun 2025 | B0 | completed | 20 of 20 (100%) | 12 (60%) | 240 | 62.8% | – | not within 20s |
| Google Cloud, 12 Jun 2025 | B1 | completed | 20 of 20 (100%) | 12 (60%) | 240 | 60.4% | – | not within 20s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | 20 of 20 (100%) | 12 (60%) | 65 | 13.0% | 12.0 s | 18.0 s |

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.
