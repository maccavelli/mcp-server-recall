#!/usr/bin/env bash
# Per-file gofmt + golint. Invoked by the agent pre-commit hook with the
# staged .go paths; can also be run as `scripts/go-precheck.sh file.go ...`.
#
# golangci-lint (the CI gate) is clean. Classic golint still reports name
# stutter on MADR/domain identifiers we keep: config.ConfigDir and
# memory.MemoryStore. Those two lines are the only allowed golint output.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: $0 <file.go> [...]" >&2
  exit 2
fi

go_files=()
for f in "$@"; do
  case "$f" in
    *.go) go_files+=("$f") ;;
  esac
done
if [ "${#go_files[@]}" -eq 0 ]; then
  exit 0
fi

unformatted="$(gofmt -l "${go_files[@]}")"
if [ -n "$unformatted" ]; then
  echo "gofmt found unformatted files:" >&2
  echo "$unformatted" >&2
  exit 1
fi

if ! command -v golint >/dev/null 2>&1; then
  echo "golint not found on PATH" >&2
  exit 1
fi

lint_out=""
for f in "${go_files[@]}"; do
  [ -f "$f" ] || continue
  out="$(golint "$f" || true)"
  [ -n "$out" ] && lint_out="${lint_out}${out}
"
done

if [ -n "$lint_out" ]; then
  filtered="$(printf '%s' "$lint_out" | grep -vE \
    -e 'internal/config/dirs\.go:.*func name will be used as config\.ConfigDir' \
    -e 'internal/memory/badger\.go:.*type name will be used as memory\.MemoryStore' \
    || true)"
  if [ -n "$filtered" ]; then
    echo "golint found issues:" >&2
    printf '%s\n' "$filtered" >&2
    exit 1
  fi
fi
