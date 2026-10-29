# Evidence fixture

`evidence.json` is the conformance suite's fixture evidence record, built from real observations, cut to the leaves of `coupon` and `v2.core.event_destination`. Source: `stripe-api-conformance` commit `c903898` (suite #75), `testdata/evidence/evidence.json`, filtered with:

```sh
jq '.leaves |= map(select(.object=="coupon" or .object=="v2.core.event_destination"))' evidence.json
```

Its `suite_commit` and run are the suite fixture's placeholders. Replace it from the same file when the suite changes the format.

`run-40926522642.json` is the suite's first evidence record from a run against Stripe, cut to the 109 leaves it proves. Source: `stripe-api-conformance` branch `results`, `evidence/761e47215607cbfaa1cf34ce674c364aae2b1e35.json` (suite run 40926522642, `passed: false` because three hand-written tests failed, so it cannot be pinned), filtered with:

```sh
jq '.leaves |= map(select(.status=="proven"))' 761e47215607cbfaa1cf34ce674c364aae2b1e35.json
```
