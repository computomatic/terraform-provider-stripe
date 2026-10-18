#!/bin/sh
# Stage the smoke bundle's build context in the directory given as $1: the wave 1
# acceptance test binaries, the sweep's test binary, test2json and the entrypoint, all for
# linux/amd64, plus the terraform binary named by $TERRAFORM_BIN (a linux/amd64 build of the
# version .github/workflows/ci.yml pins).
set -eu

out=${1:?usage: build.sh <output dir>}
terraform=${TERRAFORM_BIN:?set TERRAFORM_BIN to a linux/amd64 terraform binary}
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)

# The wave 1 packages; each becomes <name>.test and the entrypoint runs them in the order of
# the glob over those names, which is alphabetical.
wave1="billing_meter coupon customer price product webhook_endpoint balance"

rm -rf "$out"
mkdir -p "$out/bin/acc"
cd "$root"
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64

for name in $wave1; do
  go test -c -o "$out/bin/acc/$name.test" "./internal/resources/$name"
done
go test -c -o "$out/bin/sweep.test" ./internal/smoke
go build -o "$out/bin/test2json" cmd/test2json

cp "$terraform" "$out/bin/terraform"
cp "$here/Dockerfile" "$here/entrypoint.sh" "$out/"
