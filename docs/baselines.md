# Baselines B0 to B2

PushGate (B3) is judged against three baselines. Each one is a real way teams
ship config today, implemented in `rollout/` and run by `bin/rollout` and
`bin/pushbench`.

| | Checks before delivery | Delivery | Health gate | Rollback |
|---|---|---|---|---|
| **B0 push-all** | File parses and declares the right format | 100% at once | None | None |
| **B1 schema** | B0 plus a JSON Schema for the format | 100% at once | None | None |
| **B2 staged** | Same as B1 | Rings: 1%, 10%, 100% | Bake time per ring, with alarms | Automatic, to the last known good file |

## What the schemas check (B1, B2)

The schemas in `rollout/schemas/` describe what each file format promises,
written the way a team writes one from the format spec:

* `bot-features/1`: required keys, types, at least one feature, each rule a
  list of string fields.
* `quota-policy/1`: required keys and types. `quota` may be `null`, because
  policies without enforcement have none.

They deliberately do **not** encode any consumer release's limits: the
200-feature preallocation, the 20 input slots on v41 to v43, or the rule
that v43+ need a quota block when enforcement is on. Those limits live in
consumer code, which is the gap all three incidents fell into. A stricter
schema could catch some of these files, but only if someone knew to write
the rule before the incident. That is the case for checking against the
consumer itself rather than a hand-written description of it.

## B2's health gate

Modelled on AWS AppConfig deployment strategies with CloudWatch alarms.

* **Rings** are cumulative and stable: an instance's position comes from a
  hash of its id, so the 1% ring is always inside the 10% ring, and ring 0 is
  never empty. `pgctl push -percent` uses the same rings.
* **Bake time** (default 20 s on the CLI, 10 s in the benchmark) is how long
  each ring must stay healthy before the next ring gets the file. Real
  systems bake for minutes to hours; the benchmark compresses time.
* **Alarms** are evaluated every second. An alarm fires after 2 breaching
  evaluations in a row. Either signal breaches:
  * **Crash alarm:** at least 5% of exposed instances (and at least one) have
    restarted since they got the file, or are not serving. Instances get a
    1 s grace period after delivery.
  * **Error-rate alarm:** more than 5% of requests to exposed instances failed
    over the last 3 s, with at least 20 requests. Read from `loadgen -listen`,
    which plays the load balancer.
* **Rollback** writes the last known good file to every exposed instance.
  If there is no last known good file the push is **held** instead.
* A push that passes every ring becomes the new last known good. B0 and B1
  never set it, because they never checked health.

## Kill switch

```bash
bin/rollout freeze -consumer bot-proxy -reason "incident 1234"   # new pushes are refused; running ones stop before the next ring
bin/rollout rollback -consumer bot-proxy                          # last known good to every instance
bin/rollout unfreeze -consumer bot-proxy
```

A freeze stops propagation. It does not undo what is already delivered; that
is what `rollback` is for. Keeping the two separate matches how the Google
Cloud "red button" and Cloudflare's "stop propagation of new files" step
worked in their postmortems.

## What B2 cannot see

B2 only learns about a bad file by exposing instances to it. Two things make
that slow or expensive, and both show up in the week 2 results:

1. **Ring 0 may not contain a release the file breaks.** Rings are chosen
   by instance, not by release. If the 1% ring happens to hold only v44
   instances, a file that breaks v41 to v43 passes ring 0 and is caught at
   10%, or later.
2. **Some failures need specific traffic.** The CrowdStrike-shaped file only
   crashes an instance when a request reaches field 21. A quiet ring can bake
   cleanly.

PushGate's consumer-in-the-loop check (week 3) loads the file into every
release before any ring, which is aimed at exactly these two gaps.
