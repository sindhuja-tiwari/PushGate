# Consumer releases

The fleet runs four releases of each consumer at once, so PushBench can test
pushes that are safe for one release and fatal for another. Each release is a
separate binary in `bin/<consumer>/v<NN>`.

## bot-proxy (Rust)

Built with one Cargo feature per release (`--features v41` and so on).
Limits live in `consumers/rust-proxy/src/version.rs`.

| Release | Feature capacity | Input slots | Fleet share |
|---------|------------------|-------------|-------------|
| v41     | 128              | 20          | 5%          |
| v42     | 200              | 20          | 15%         |
| v43     | 200              | 20          | 30%         |
| v44     | 200              | 21          | 50%         |

* **Feature capacity.** The table is preallocated. A file with more features
  fails to load, and the loader unwraps the error, so the process panics.
  This is the Cloudflare November 2025 shape. v41's smaller limit means a
  normal 15% growth in features is safe for 95% of the fleet and fatal for 5%.
* **Input slots.** Rules are templates of match fields, read against a fixed
  array of request inputs. A rule with a 21st field indexes past the end on
  v41 to v43 and panics, but only when a request reaches that field. This is
  the CrowdStrike July 2024 shape. v44 has the extra slot.

## go-policy (Go)

Built with `-ldflags "-X main.buildVersion=NN"`.

| Release | Quota enforcement | Fleet share |
|---------|-------------------|-------------|
| v41     | no                | 10%         |
| v42     | no                | 20%         |
| v43     | yes               | 30%         |
| v44     | yes               | 40%         |

* **Quota enforcement.** v43 and later read `policy.quota` without a nil
  check when `enforce_quota` is true. A policy row with a blank quota block
  crashes them on load and on every restart. v41 and v42 never read the field.
  This is the Google Cloud June 2025 shape.

## What the consumers do not have yet

No last-known-good fallback, no declared limits and no fail-open setting.
That library arrives in week 4. Until then the consumers fail the way the
originals did, which is what baselines B0 to B2 measure.
