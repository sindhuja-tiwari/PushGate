# Good-push corpus

Generated 2026-11-05 17:38 UTC. 500 safe files (seed 1), half feature files and half policy files, each with one to three routine changes. Every file is within every release's limits, so every hold is a false hold.

| Check | Held | False-hold rate (95% CI) | Passed with a warning | Gate time p50 / p95 |
|---|---|---|---|---|
| B1 schema | 0 of 500 | 0.0% (0.0 to 0.8%) | – | – |
| Gate: semantic diff only | 0 of 500 | 0.0% (0.0 to 0.8%) | 83 (17%) | 1 / 1 ms |
| Gate: sandbox only | 0 of 500 | 0.0% (0.0 to 0.8%) | 55 (11%) | 95 / 119 ms |
| Gate: both layers (B3) | 0 of 500 | 0.0% (0.0 to 0.8%) | 138 (28%) | 95 / 119 ms |

Warnings by check (both layers; a file can have several):

- `diff/new-shape`: 83 files
- `sandbox/outputs`: 55 files

Gate cost: 0.13 CPU-seconds per push on average (p95 0.15), for 8 sandboxed processes per check (4 releases, candidate and last known good).
