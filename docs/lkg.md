# The last-known-good library

The gate (B3, [gate.md](gate.md)) stops bad files before they ship. This
library is the second line: it lives inside each consumer, so a bad file
that gets past every check still cannot take the process down. It comes in
two versions with the same behavior and the same status format:

- Go: `lib/go/lkg` (used by go-policy)
- Rust: `lib/rust/lkg` (used by bot-proxy)

Both consumers keep their original naive loaders by default, so the week 1
to 3 baselines stay exactly as they were. `--on-bad-file keep_last_good` or
`--on-bad-file fail_closed` switches a consumer to the library. The fleet
passes it to every instance with `bin/fleet -on-bad-file MODE`, or per
consumer with `on_bad_file` in `deploy/fleet.json`.

## What it does

| Behavior | How |
|---|---|
| **Keep the last known good** | A new file that fails to parse, breaks a declared limit or (Go) panics while loading is rejected. The process keeps serving the previous file. |
| **Survive restarts** | Every good file is cached at `<config>.lkg`. A process that restarts with a bad file on disk serves the cache instead of crash-looping. In the Google incident the same file crashed each restart; here the restart serves the cache. |
| **Declared limits** | The consumer's parse function checks the limits its own code relies on. For bot-proxy those are its feature capacity and its input slots, so a 21-field rule is refused at load instead of reading past the end of the array on a later request. |
| **Catch panics (Go)** | Parsing runs under `recover`. go-policy's unchanged v43 code still dereferences the blank quota, but the library turns that into a rejection: `panic while loading: ... nil pointer dereference at main.go:101`. |
| **Fail open or closed** | `keep_last_good` keeps serving the old file, so availability comes first. `fail_closed` stops answering (HTTP 503) until a good file arrives, for files where stale rules are worse than no answer. |
| **Randomized reload** | A changed file is read after a random delay of up to one poll interval, so a fleet does not reload in lockstep. Read errors, such as a file caught mid-replace, are retried with full-jitter exponential backoff (250 ms up to 30 s). A file that is read but rejected is not retried until it changes. |
| **Report** | `/healthz` includes the library's status: what is served, from where, how many loads and rejections, the last rejection with its reason, and the declared limits. |

Status, as `/healthz` shows it under `"lkg"`:

```json
{
  "mode": "keep_last_good",
  "serving": true,
  "config_version": "feat-20261029T145811Z-94ed8b2c00",
  "from": "file",
  "loads": 1,
  "rejections": 1,
  "last_rejected": {
    "config_version": "feat-20261031T165422Z-90c598411e",
    "reason": "feature 0 (feat_000) has 21 match fields; this release reads 20 input slots",
    "at": "2026-11-04T10:12:03.511Z"
  },
  "limits": {"input_slots": 20, "max_features": 200}
}
```

## Rust panics cannot be caught here

bot-proxy is built with `panic = "abort"`, like many production Rust
services, so the Rust library cannot catch a panic the way the Go one does.
In Rust, declared limits are the only guard, and they have to cover every
limit the serving code depends on. That is why bot-proxy's checked loader
tests both the feature count and the rule width. A limit nobody declared
still crashes the process. The gate's sandbox is what catches those.

## Rejections are a health signal

A consumer that refuses a file and keeps serving looks healthy: it does not
crash, and its requests succeed. Without extra help, a staged rollout would
carry the file to 100% while every vulnerable instance quietly serves stale
config. So the supervisor copies each instance's last rejection into the
registry, and the rollout's alarm counts **an instance that refused the
candidate file** as a failure, the same as a crash. The alarm then reads, for
example:

```
rejection alarm: 2 of 4 exposed instances crashed, refused the file or are not serving
(e.g. bp-eu-west-017 v43 up, refused feat-...0e: feature 0 (feat_000) has 21 match fields; this release reads 20 input slots)
```

`bin/pgctl rejections` is the fleet-wide view, grouped by release and
reason, with what each group is still serving:

```
 120 instances  bot-proxy v43 refused feat-...0e: feature 0 (feat_000) has 21 match fields; this release reads 20 input slots
      still serving: feat-...00
  28 instances  go-policy v43 refused pol-...c3: panic while loading: runtime error: invalid memory address or nil pointer dereference at main.go:101
      still serving: pol-...80
```

## Using it

Go:

```go
l := lkg.Open(lkg.Options{
    Path:   "policy.json",
    Mode:   lkg.KeepLastGood,
    Limits: map[string]any{"quota_enforcement": true},
}, parse)                     // parse func([]byte) (*index, error)
go l.Watch(ctx)
ix, ok := l.Current()         // ok is false when there is nothing to serve
```

Rust:

```rust
let mut opts = lkg::Options::new("feature.json", lkg::Mode::KeepLastGood);
opts.limits.insert("max_features".into(), 200.into());
let loader = lkg::Loader::open(opts, parse, |msg| eprintln!("{msg}"));
loader.watch();
let table = loader.current(); // None when there is nothing to serve
```

## Limits

- **It needs a code release.** Old releases do not have the library. In this
  simulation every release gets it through a flag, which is the same as
  assuming it was backported. In a real fleet it covers only the releases
  that ship it, which is why the gate stays in front.
- **No good file, nothing to serve.** If the very first file is bad and
  there is no cache, both modes refuse requests until a good file arrives.
- **Stale is not always safe.** Keeping an old file is right for a bot
  model. For a revocation list or a security rule set, serving stale data can
  be the larger risk, so choose `fail_closed` for those.
