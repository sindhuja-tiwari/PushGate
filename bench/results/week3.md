# PushBench results

Generated 2026-11-01 19:30 UTC on 4 CPUs. Traffic 2000 req/s. Each run watched the fleet for 45s after the push. B2 bake time 10s per ring (rings 1%, 10%, 100%).

| Incident | Strategy | Outcome | Gate | Got the bad file | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|---|
| Cloudflare, 18 Nov 2025 | B0 | completed | – | 400 of 400 (100%) | 400 (100%) | 17976 | 100.0% | – | not within 45s |
| Cloudflare, 18 Nov 2025 | B1 | completed | – | 400 of 400 (100%) | 400 (100%) | 17972 | 99.6% | – | not within 45s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | – | 4 of 400 (1%) | 4 (1%) | 24 | 0.1% | 2.0 s | 8.0 s |
| Cloudflare, 18 Nov 2025 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 400 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B3-DIFF | rejected before delivery | held by diff, 0.0 s | 0 of 400 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B3-SANDBOX | rejected before delivery | held by sandbox, 0.2 s | 0 of 400 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B0 | completed | – | 400 of 400 (100%) | 200 (50%) | 8602 | 45.8% | – | not within 45s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | – | 400 of 400 (100%) | 200 (50%) | 8659 | 46.7% | – | not within 45s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | – | 4 of 400 (1%) | 2 (0%) | 8 | 0.0% | 3.0 s | 6.5 s |
| CrowdStrike, 19 Jul 2024 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 400 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B3-DIFF | rolled_back | passed, 0.0 s | 4 of 400 (1%) | 2 (0%) | 4 | 0.0% | 2.0 s | 3.5 s |
| CrowdStrike, 19 Jul 2024 | B3-SANDBOX | rejected before delivery | held by sandbox, 0.2 s | 0 of 400 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B0 | completed | – | 100 of 100 (100%) | 68 (68%) | 3056 | 68.3% | – | not within 45s |
| Google Cloud, 12 Jun 2025 | B1 | completed | – | 100 of 100 (100%) | 68 (68%) | 3058 | 68.2% | – | not within 45s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | – | 1 of 100 (1%) | 1 (1%) | 6 | 0.1% | 2.0 s | 6.0 s |
| Google Cloud, 12 Jun 2025 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 100 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B3-DIFF | rolled_back | passed, 0.0 s | 1 of 100 (1%) | 1 (1%) | 4 | 0.0% | 2.0 s | 4.5 s |
| Google Cloud, 12 Jun 2025 | B3-SANDBOX | rejected before delivery | held by sandbox, 0.3 s | 0 of 100 (0%) | 0 (0%) | 0 | 0.0% | – | never failed |

Gate: what B3's pre-exposure gate decided, which layer held the file, and how long the gate took. B3-DIFF and B3-SANDBOX run one layer each.

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.
