#!/usr/bin/env bash
# Fails when docs/ is not what `go generate ./...` produces from the schema and the
# examples. Compares against a copy taken before regenerating, so it works on uncommitted
# changes. Needs a terraform (or tofu) binary on the path.
set -euo pipefail

cd "$(dirname "$0")/.."
before="$(mktemp -d)"
trap 'rm -rf "$before"' EXIT
cp -R docs/. "$before"

go generate ./...

if ! diff -r "$before" docs >/dev/null; then
	echo "docs/ is out of date. It has been regenerated; review and commit the result:" >&2
	diff -rq "$before" docs >&2 || true
	exit 1
fi
