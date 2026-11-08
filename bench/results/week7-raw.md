# PushBench results

Generated 2026-11-07 22:06 UTC on 4 CPUs. Traffic 1000 req/s. Each run watched the fleet for 25s after the push. B2 bake time 5s per ring (rings 1%, 10%, 100%).

Consumers: original naive loaders (no last-known-good library).

| Incident | Strategy | Outcome | Gate | Got the bad file | Refused it | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |
|---|---|---|---|---|---|---|---|---|---|---|
| Cloudflare, 18 Nov 2025 | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1999 | 100.0% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 9 | 0.3% | 2.0 s | 10.0 s |
| Cloudflare, 18 Nov 2025 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.2% | – | not within 25s |
| Cloudflare variant | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.0% | – | not within 25s |
| Cloudflare variant | B2 | rolled_back | – | 80 of 80 (100%) | – | 4 (5%) | 20 | 0.8% | 12.0 s | 18.0 s |
| Cloudflare variant | B3 | rejected before delivery | held by diff + sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B0 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 976 | 47.3% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 990 | 49.3% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 4 | 0.2% | 2.0 s | 4.0 s |
| CrowdStrike, 19 Jul 2024 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | B0 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 2 | 0.0% | – | 19.0 s |
| CrowdStrike variant | B1 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | B2 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 2 | 0.0% | – | 24.5 s |
| CrowdStrike variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1998 | 100.0% | – | not within 25s |
| Growth past the limit | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1998 | 100.0% | – | not within 25s |
| Growth past the limit | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 4 | 0.1% | 2.0 s | 4.0 s |
| Growth past the limit | B3 | rejected before delivery | held by diff + sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B0 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B1 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B2 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B3 | completed | passed, 0.3 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B0 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B1 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B2 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B3 | completed | passed, 0.4 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.4% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.9% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | – | 20 of 20 (100%) | – | 12 (60%) | 60 | 10.8% | 12.0 s | 18.0 s |
| Google Cloud, 12 Jun 2025 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 60.1% | – | not within 25s |
| Google Cloud variant | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 60.0% | – | not within 25s |
| Google Cloud variant | B2 | rolled_back | – | 20 of 20 (100%) | – | 12 (60%) | 70 | 12.1% | 12.0 s | 20.0 s |
| Google Cloud variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 4.9% | – | not within 25s |
| Normal growth, old release | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 4.9% | – | not within 25s |
| Normal growth, old release | B2 | rolled_back | – | 80 of 80 (100%) | – | 4 (5%) | 24 | 1.2% | 12.0 s | 20.0 s |
| Normal growth, old release | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B0 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Type error | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 6 | 0.2% | 2.0 s | 6.0 s |
| Cloudflare, 18 Nov 2025 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.0% | – | not within 25s |
| Cloudflare variant | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 4.8% | – | not within 25s |
| Cloudflare variant | B2 | rolled_back | – | 8 of 80 (10%) | – | 1 (1%) | 6 | 0.3% | 7.0 s | 12.0 s |
| Cloudflare variant | B3 | rejected before delivery | held by diff + sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B0 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 992 | 48.3% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 991 | 48.6% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | – | 8 of 80 (10%) | – | 2 (2%) | 14 | 0.6% | 7.0 s | 14.0 s |
| CrowdStrike, 19 Jul 2024 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | B0 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 4 | 0.0% | – | 21.0 s |
| CrowdStrike variant | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 5 | 0.0% | – | 24.0 s |
| CrowdStrike variant | B2 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 4 | 0.0% | – | 21.0 s |
| CrowdStrike variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Growth past the limit | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1998 | 100.0% | – | not within 25s |
| Growth past the limit | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 6 | 0.2% | 2.0 s | 6.0 s |
| Growth past the limit | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B0 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B1 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B2 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B3 | completed | passed, 0.2 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B0 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B1 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B2 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B3 | completed | passed, 0.3 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.8% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.6% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | – | 1 of 20 (5%) | – | 1 (5%) | 4 | 0.7% | 2.0 s | 4.0 s |
| Google Cloud, 12 Jun 2025 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.8% | – | not within 25s |
| Google Cloud variant | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.9% | – | not within 25s |
| Google Cloud variant | B2 | rolled_back | – | 1 of 20 (5%) | – | 1 (5%) | 10 | 1.8% | 2.0 s | 10.0 s |
| Google Cloud variant | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 4.8% | – | not within 25s |
| Normal growth, old release | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.0% | – | not within 25s |
| Normal growth, old release | B2 | rolled_back | – | 8 of 80 (10%) | – | 1 (1%) | 4 | 0.1% | 7.0 s | 10.0 s |
| Normal growth, old release | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B0 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Type error | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 99.8% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1998 | 100.0% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 8 | 0.4% | 2.0 s | 8.0 s |
| Cloudflare, 18 Nov 2025 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.0% | – | not within 25s |
| Cloudflare variant | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 4.8% | – | not within 25s |
| Cloudflare variant | B2 | rolled_back | – | 8 of 80 (10%) | – | 1 (1%) | 4 | 0.2% | 7.0 s | 10.0 s |
| Cloudflare variant | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B0 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 983 | 47.5% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 988 | 48.7% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | – | 8 of 80 (10%) | – | 5 (6%) | 37 | 1.8% | 7.0 s | 14.0 s |
| CrowdStrike, 19 Jul 2024 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | B0 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | B1 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 4 | 0.1% | – | 21.5 s |
| CrowdStrike variant | B2 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 2 | 0.0% | – | 14.5 s |
| CrowdStrike variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1999 | 99.6% | – | not within 25s |
| Growth past the limit | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Growth past the limit | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 10 | 0.4% | 2.0 s | 10.0 s |
| Growth past the limit | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B0 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B1 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B2 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B3 | completed | passed, 0.3 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B0 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B1 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B2 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B3 | completed | passed, 0.3 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.0% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 58.9% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | – | 2 of 20 (10%) | – | 1 (5%) | 6 | 1.3% | 7.0 s | 12.0 s |
| Google Cloud, 12 Jun 2025 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 60.6% | – | not within 25s |
| Google Cloud variant | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 60.4% | – | not within 25s |
| Google Cloud variant | B2 | rolled_back | – | 2 of 20 (10%) | – | 1 (5%) | 8 | 2.1% | 7.0 s | 14.0 s |
| Google Cloud variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.1% | – | not within 25s |
| Normal growth, old release | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.3% | – | not within 25s |
| Normal growth, old release | B2 | rolled_back | – | 8 of 80 (10%) | – | 1 (1%) | 4 | 0.2% | 7.0 s | 10.0 s |
| Normal growth, old release | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B0 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1998 | 100.0% | – | not within 25s |
| Type error | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1999 | 100.0% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 10 | 0.3% | 2.0 s | 10.0 s |
| Cloudflare, 18 Nov 2025 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 4.9% | – | not within 25s |
| Cloudflare variant | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 4.9% | – | not within 25s |
| Cloudflare variant | B2 | rolled_back | – | 8 of 80 (10%) | – | 1 (1%) | 6 | 0.3% | 7.0 s | 12.0 s |
| Cloudflare variant | B3 | rejected before delivery | held by diff + sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B0 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 985 | 47.7% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 981 | 49.1% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | – | 8 of 80 (10%) | – | 4 (5%) | 30 | 1.4% | 7.0 s | 14.0 s |
| CrowdStrike, 19 Jul 2024 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | B0 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | B1 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 3 | 0.0% | – | 18.0 s |
| CrowdStrike variant | B2 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 3 | 0.0% | – | 25.0 s |
| CrowdStrike variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1998 | 100.0% | – | not within 25s |
| Growth past the limit | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Growth past the limit | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 8 | 0.3% | 2.0 s | 8.0 s |
| Growth past the limit | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B0 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B1 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B2 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B3 | completed | passed, 0.3 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B0 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B1 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B2 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B3 | completed | passed, 0.4 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 60.1% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 60.9% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | – | 1 of 20 (5%) | – | 1 (5%) | 10 | 2.1% | 2.0 s | 10.0 s |
| Google Cloud, 12 Jun 2025 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 61.1% | – | not within 25s |
| Google Cloud variant | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 60.1% | – | not within 25s |
| Google Cloud variant | B2 | rolled_back | – | 1 of 20 (5%) | – | 1 (5%) | 8 | 1.5% | 2.0 s | 8.0 s |
| Google Cloud variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.0% | – | not within 25s |
| Normal growth, old release | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.2% | – | not within 25s |
| Normal growth, old release | B2 | rolled_back | – | 8 of 80 (10%) | – | 1 (1%) | 8 | 0.3% | 7.0 s | 14.0 s |
| Normal growth, old release | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B0 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Type error | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare, 18 Nov 2025 | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 2000 | 100.0% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1998 | 99.7% | – | not within 25s |
| Cloudflare, 18 Nov 2025 | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 6 | 0.2% | 2.0 s | 6.0 s |
| Cloudflare, 18 Nov 2025 | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Cloudflare variant | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.1% | – | not within 25s |
| Cloudflare variant | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.0% | – | not within 25s |
| Cloudflare variant | B2 | rolled_back | – | 8 of 80 (10%) | – | 1 (1%) | 4 | 0.2% | 7.0 s | 10.0 s |
| Cloudflare variant | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike, 19 Jul 2024 | B0 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 985 | 47.2% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B1 | completed | – | 80 of 80 (100%) | – | 40 (50%) | 985 | 48.5% | – | not within 25s |
| CrowdStrike, 19 Jul 2024 | B2 | rolled_back | – | 8 of 80 (10%) | – | 5 (6%) | 22 | 1.0% | 7.0 s | 12.0 s |
| CrowdStrike, 19 Jul 2024 | B3 | rejected before delivery | held by sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| CrowdStrike variant | B0 | completed | – | 80 of 80 (100%) | – | 2 (2%) | 7 | 0.1% | – | 25.0 s |
| CrowdStrike variant | B1 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 3 | 0.0% | – | 15.5 s |
| CrowdStrike variant | B2 | completed | – | 80 of 80 (100%) | – | 1 (1%) | 5 | 0.1% | – | 23.0 s |
| CrowdStrike variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Growth past the limit | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1999 | 99.9% | – | not within 25s |
| Growth past the limit | B1 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1998 | 100.0% | – | not within 25s |
| Growth past the limit | B2 | rolled_back | – | 1 of 80 (1%) | – | 1 (1%) | 6 | 0.2% | 2.0 s | 6.0 s |
| Growth past the limit | B3 | rejected before delivery | held by diff + sandbox, 0.2 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B0 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B1 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B2 | completed | – | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 10 new features | B3 | completed | passed, 0.3 s | 80 of 80 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B0 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B1 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B2 | completed | – | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Good push: 5 new projects | B3 | completed | passed, 0.4 s | 20 of 20 (100%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud, 12 Jun 2025 | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 60.0% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.5% | – | not within 25s |
| Google Cloud, 12 Jun 2025 | B2 | rolled_back | – | 20 of 20 (100%) | – | 12 (60%) | 78 | 13.9% | 12.0 s | 20.0 s |
| Google Cloud, 12 Jun 2025 | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Google Cloud variant | B0 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 59.0% | – | not within 25s |
| Google Cloud variant | B1 | completed | – | 20 of 20 (100%) | – | 12 (60%) | 300 | 58.9% | – | not within 25s |
| Google Cloud variant | B2 | rolled_back | – | 20 of 20 (100%) | – | 12 (60%) | 86 | 14.5% | 12.0 s | 20.0 s |
| Google Cloud variant | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 20 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Normal growth, old release | B0 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 5.0% | – | not within 25s |
| Normal growth, old release | B1 | completed | – | 80 of 80 (100%) | – | 4 (5%) | 100 | 4.9% | – | not within 25s |
| Normal growth, old release | B2 | rolled_back | – | 8 of 80 (10%) | – | 1 (1%) | 4 | 0.2% | 7.0 s | 10.0 s |
| Normal growth, old release | B3 | rejected before delivery | held by sandbox, 0.3 s | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B0 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Half-written file | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B0 | completed | – | 80 of 80 (100%) | – | 80 (100%) | 1999 | 100.0% | – | not within 25s |
| Type error | B1 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B2 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |
| Type error | B3 | rejected before delivery | – | 0 of 80 (0%) | – | 0 (0%) | 0 | 0.0% | – | never failed |

Refused it: the most instances at once whose last-known-good library rejected the bad file. With keep_last_good they keep serving the old file, so they are not counted as failing; with fail_closed they stop answering and are.

Gate: what B3's pre-exposure gate decided, which layer held the file, and how long the gate took. B3-DIFF and B3-SANDBOX run one layer each.

B0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.
