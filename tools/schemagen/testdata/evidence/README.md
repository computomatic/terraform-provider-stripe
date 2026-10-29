# Evidence fixture

`evidence.json` is the conformance suite's fixture evidence record, built from real observations, cut to the leaves of `coupon` and `v2.core.event_destination`. Source: `stripe-api-conformance` commit `c903898` (suite #75), `testdata/evidence/evidence.json`, filtered with:

```sh
jq '.leaves |= map(select(.object=="coupon" or .object=="v2.core.event_destination"))' evidence.json
```

Its `suite_commit` and run are the suite fixture's placeholders. Replace it from the same file when the suite changes the format.
