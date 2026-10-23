#!/bin/sh
# Run the sandbox smoke: the sweep, each wave 1 package's acceptance tests one at a time,
# and the sweep again whatever happened. The test output goes to stdout as test2json events,
# one stream for the whole run; this script's own messages go to stderr. Exit status is
# non-zero if any test run or either sweep failed.
#
# The harness checks the key (STRIPE_API_KEY, an sk_test_ key) itself; this script never
# reads it and never prints the environment.
set -u

if [ "${TF_SMOKE:-}" != "1" ]; then
  echo "smoke: refusing to start: set TF_SMOKE=1 to run against a Stripe sandbox" >&2
  exit 2
fi

status_file=$(mktemp)
trap 'rm -f "$status_file"' EXIT
failed=0

# run <name> <command...>: run the command with its output as test2json events for the
# package <name>. A shell without pipefail loses the command's status in a pipe, so the
# status goes through a file.
run() {
  name=$1
  shift
  echo "smoke: running $name" >&2
  { "$@" 2>&1; echo $? > "$status_file"; } | test2json -p "$name"
  code=$(cat "$status_file")
  if [ "$code" != "0" ]; then
    echo "smoke: $name failed (exit $code)" >&2
    failed=1
  fi
}

sweep() {
  run "sweep ($1)" /smoke/sweep.test -test.run '^TestSmokeSweep$' -test.timeout 10m -test.v
}

sweep before
export TF_ACC=1
for binary in /smoke/acc/*.test; do
  name=$(basename "$binary" .test)
  run "$name" "$binary" -test.run '^TestAcc' -test.parallel 1 -test.timeout 10m -test.v
done
sweep after

exit "$failed"
