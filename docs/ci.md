# PushGate in CI (week 8)

The gate does not need the simulated fleet. `gate ci` runs it on any
repository: for every config file a pull request changes, it starts each
release of the service that loads that file, replays sampled requests, and
compares the result with the same release running the base branch's version
of the file. A file that crashes, leaks or slows down any release fails the
check, and the job summary says which releases and why.

## Set it up

1. Describe your config files and the releases that load them in
   `pushgate.json` at the repository root:

   ```json
   {
     "consumers": [
       {
         "name": "ratelimiter",
         "files": ["config/limits.json"],
         "releases": [
           {"version": "v1", "instances": 30, "command": ["python3", "server.py", "--release", "1", "--port", "{port}", "--config", "{config}"]},
           {"version": "v2", "instances": 70, "command": ["docker", "run", "--rm", "--network", "host", "-v", "{config}:/etc/limits.json:ro", "ghcr.io/acme/ratelimiter:v2", "--port", "{port}"]}
         ],
         "traffic": "ci/ratelimiter-requests.txt",
         "answer": "allowed",
         "rows": "limits",
         "key": ["route"]
       }
     ]
   }
   ```

2. Add the action to a workflow:

   ```yaml
   on: pull_request
   jobs:
     pushgate:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
           with:
             fetch-depth: 0          # the gate reads the base branch's version of each file
         - uses: sindhuja-tiwari/PushGate@v1
           with:
             spec: pushgate.json
   ```

   Build or pull whatever the release commands need in an earlier step. The
   action builds the gate itself and needs only Go, which it sets up.

## The spec

| Field | Meaning |
|---|---|
| `name` | The consumer's name, used in reports. |
| `files` | Path globs, relative to the repository root, of the config files this consumer loads. `**` matches any number of directories. |
| `releases` | Every release running in production now. `command` is the command line, with `{port}` and `{config}` filled in per run, and optionally `{id}`. `instances` sizes the blast radius in the report. |
| `traffic` | A sample of requests to replay, one per line, as a path such as `/limit?route=/api/users`. A loadgen `-record` file also works. Without one, the gate only checks that each release loads the file. |
| `replay` | How many sampled requests to replay (default 1,000). |
| `health` | Path that answers 200 once the service is serving (default `/healthz`). If the command has `{id}`, the answer must also contain `"id":"<that id>"`, which proves the gate is talking to its own process. |
| `answer` | The JSON field of each response compared between candidate and last known good. More than 10% changed answers is a warning. |
| `format` | `bot-features` or `quota-policy` turns on PushBench's built-in semantic diff and requests built for every changed rule. |
| `rows`, `key` | For any other JSON file: the top-level array of rows and the fields that identify a row. The gate then holds on duplicate keys and on the row count changing by more than 50%, and warns above 20%. |

## What it holds on

The same rules as the gate in front of the fleet ([gate.md](gate.md)): a
release crashing on load or on a replayed request, more than 1% of requests
failing, p99 latency more than 3x and 20 ms above the last known good file,
memory growing with every request, duplicate keys, or the file doubling or
halving. Changed answers, memory over 2x and smaller size changes are
warnings. A hold fails the job, adds an error annotation on the file, and
writes the report to the job summary.

## Run it locally

```bash
go build -o bin/gate ./gate/cmd/gate
bin/gate ci -spec pushgate.json -base origin/main          # changed files, like the action
bin/gate ci -spec pushgate.json -consumer ratelimiter -files new.json -lkg config/limits.json
```

`gate ci` exits 0 when every file passes, 1 when any is held and 2 on errors.

## In this repository

`examples/ci/` has a spec for three consumers: PushBench's bot-proxy and
go-policy, and a 70-line Python rate limiter whose release 1 divides by a
limit's `burst`. The `pushgate` workflow runs the action on any pull request
that touches `examples/ci/`, and `scripts/test_gate_ci.sh` (run in CI)
checks that known-bad files are held and known-good files pass:

```
ok    ratelimiter: burst 0 crashes release 1 on load (exit 1)
ok    ratelimiter: a raised limit passes (exit 0)
ok    ratelimiter: every route twice is held by the generic diff (exit 1)
ok    bot-proxy: rule pinned to a rare request is held (exit 1)
ok    bot-proxy: traced rule that leaks on v44 is held (exit 1)
ok    bot-proxy: unchanged file passes (exit 0)
ok    go-policy: blank quota is held (exit 1)
```

To see a hold in a real pull request, change one `burst` in
`examples/ci/config/limits.json` to 0.

## Limits

- **The releases must run on the CI machine.** Containers work through
  `docker run` in the command. A service that needs a database or other
  dependencies to start needs those in the job too, or a mode that loads
  config without them.
- **The traffic sample is yours to keep fresh.** The gate replays what the
  file holds. For the built-in formats it also builds requests for every
  changed rule; for other formats, a changed entry that no sampled request
  reaches is only checked for loading.
- **It checks the releases you list.** Keep `releases` in step with what is
  deployed, ideally generated by your deploy tooling.
