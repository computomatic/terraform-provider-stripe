#!/usr/bin/env bash
# PostToolUse hook for Write and Edit. Rejects newly written text that contains an em-dash or a spaced double hyphen,
# which the owner's writing style forbids. Only the text just written is checked, so pre-existing lines never trip it.
set -uo pipefail
input=$(cat)
path=$(printf '%s' "$input" | jq -r '.tool_input.file_path // ""')
text=$(printf '%s' "$input" | jq -r '.tool_input.content // .tool_input.new_string // ""')
case "$path" in
  */vendor/*|*/third-party-docs/*|*/node_modules/*|*.lock.hcl|*go.sum|*.json) exit 0 ;;
esac
if printf '%s' "$text" | grep -q '—' || printf '%s' "$text" | grep -qE '(^|[[:space:]])--([[:space:]]|$)'; then
  echo "Style rule: no em-dashes and no double hyphens used as dashes. Rewrite the sentence in $path so it needs neither." >&2
  exit 2
fi
exit 0
