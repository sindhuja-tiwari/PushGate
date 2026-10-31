# PushBench results

Generated 2026-10-31 16:53 UTC on 4 CPUs. Traffic 2000 req/s. Each run watched the fleet for 45s after the push. B2 bake time 10s per ring (rings 1%, 10%, 100%).

| Incident | Strategy | Outcome | Got the bad file | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|
| Cloudflare, 18 Nov 2025 | B0 | completed | 400 of 400 (100%) | 400 (100%) | 17940 | 100.0% | – | not within 45s |
| Cloudflare, 18 Nov 2025 | B1 | completed | 400 of 400 (100%) | 400 (100%) | 17964 | 100.0% | – | not within 45s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | 4 of 400 (1%) | 4 (1%) | 22 | 0.1% | 2.0 s | 8.0 s |
| CrowdStrike, 19 Jul 2024 | B0 | completed | 400 of 400 (100%) | 200 (50%) | 8582 | 46.3% | – | not within 45s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | 400 of 400 (100%) | 200 (50%) | 8680 | 46.9% | – | not within 45s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | 4 of 400 (1%) | 2 (0%) | 10 | 0.1% | 2.0 s | 8.0 s |
| Google Cloud, 12 Jun 2025 | B0 | completed | 100 of 100 (100%) | 68 (68%) | 3060 | 68.3% | – | not within 45s |
| Google Cloud, 12 Jun 2025 | B1 | completed | 100 of 100 (100%) | 68 (68%) | 3058 | 67.6% | – | not within 45s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | 1 of 100 (1%) | 1 (1%) | 6 | 0.2% | 2.0 s | 6.5 s |

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.
