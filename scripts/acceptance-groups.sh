#!/usr/bin/env bash
# Splits the acceptance test packages (./internal/resources/...) into the given number of
# groups, for CI to run in parallel, and prints them as a JSON array of
# {"index": <1..n>, "packages": "<space-separated package paths>"}.
#
# A package's weight is the number of top-level tests in its test files, which tracks its
# running time well enough. The heaviest package goes first, to the lightest group so far,
# with ties broken by name, so the same tree always gives the same groups. Fails unless
# every package lands in exactly one group and no group is empty.
set -euo pipefail
shopt -s nullglob

cd "$(dirname "$0")/.."

count="${1:-}"
if ! [[ "$count" =~ ^[1-9][0-9]*$ ]]; then
	echo "usage: $0 <number of groups>" >&2
	exit 2
fi

module="$(go list -m)"
packages="$(go list -find -f '{{.ImportPath}}' ./internal/resources/... | sed "s|^${module}|.|" | sort)"

weighted=""
while read -r package; do
	files=("$package"/*_test.go)
	weight=0
	if [ "${#files[@]}" -gt 0 ]; then
		weight="$(awk '/^func Test/ { n++ } END { print n + 0 }' "${files[@]}")"
	fi
	weighted+="${weight} ${package}"$'\n'
done <<< "$packages"

groups="$(printf '%s' "$weighted" | sort -k1,1nr -k2,2 | awk -v count="$count" '
	{
		lightest = 1
		for (g = 2; g <= count; g++) if (load[g] < load[lightest]) lightest = g
		load[lightest] += $1
		members[lightest] = members[lightest] " " $2
	}
	END { for (g = 1; g <= count; g++) print g members[g] }
')"

if grep -qx '[0-9]*' <<< "$groups"; then
	echo "A group is empty: ask for fewer than ${count} groups." >&2
	exit 1
fi

assigned="$(cut -s -d' ' -f2- <<< "$groups" | tr ' ' '\n' | sed '/^$/d' | sort)"
if [ "$assigned" != "$packages" ]; then
	echo "The groups do not hold every package exactly once:" >&2
	diff <(echo "$packages") <(echo "$assigned") >&2 || true
	exit 1
fi

jq -cRn '[inputs | split(" ") | {index: (.[0] | tonumber), packages: (.[1:] | join(" "))}]' <<< "$groups"
