#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

minimum="${1:-35}"
profile="$(mktemp "${TMPDIR:-/tmp}/sah-cli-cover.XXXXXX")"
trap 'rm -f "$profile"' EXIT

go test -coverprofile="$profile" ./...

total="$(
	go tool cover -func="$profile" |
	awk '/^total:/ {gsub(/%/, "", $3); print $3}'
)"

if [ -z "$total" ]; then
	echo "failed to read total coverage from go tool cover output" >&2
	exit 1
fi

if ! awk -v total="$total" -v minimum="$minimum" 'BEGIN { exit !(total + 0 >= minimum + 0) }'; then
	printf 'coverage %.1f%% is below required minimum %.1f%%\n' "$total" "$minimum" >&2
	exit 1
fi

printf 'coverage %.1f%% meets required minimum %.1f%%\n' "$total" "$minimum"
