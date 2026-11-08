# PushBench: the urgent lane (b3-urgent)

Generated 2026-11-08 07:03 UTC on 4 CPUs. Traffic 1000 req/s. Each run watched the fleet for 25s after the push, unless its scenario sets a longer window. B2 bake time 5s per ring (rings 1%, 10%, 100%).

Consumers: original naive loaders (no last-known-good library).

| Incident | Seed | Strategy | Outcome | Gate | Got the bad file | Refused it | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Cloudflare, 18 Nov 2025 | 1 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | 1 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | 1 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | 1 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | 1 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | 1 | B3-URGENT | completed | passed, 0.3 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | 1 | B3-URGENT | completed | passed, 0.5 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | 1 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | 1 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | 1 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | 1 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | 1 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | 2 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | 2 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | 2 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | 2 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | 2 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | 2 | B3-URGENT | completed | passed, 0.2 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | 2 | B3-URGENT | completed | passed, 0.4 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | 2 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | 2 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | 2 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | 2 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | 2 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | 3 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | 3 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | 3 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | 3 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | 3 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | 3 | B3-URGENT | completed | passed, 0.3 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | 3 | B3-URGENT | completed | passed, 0.3 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | 3 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | 3 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | 3 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | 3 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | 3 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | 4 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | 4 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | 4 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | 4 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | 4 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | 4 | B3-URGENT | completed | passed, 0.2 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | 4 | B3-URGENT | completed | passed, 0.4 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | 4 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | 4 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | 4 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | 4 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | 4 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | 5 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | 5 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | 5 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | 5 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | 5 | B3-URGENT | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | 5 | B3-URGENT | completed | passed, 0.3 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | 5 | B3-URGENT | completed | passed, 0.3 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | 5 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | 5 | B3-URGENT | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | 5 | B3-URGENT | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | 5 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | 5 | B3-URGENT | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |

Refused it: the most instances at once whose last-known-good library rejected the bad file. With keep_last_good they keep serving the old file, so they are not counted as failing; with fail_closed they stop answering and are.

Gate: what B3's pre-exposure gate decided, which layer held the file, and how long the gate took. B3-DIFF and B3-SANDBOX run one layer each.

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.
