#!/bin/sh
# Holds docs/0008-MADR-stamp-tag-builds-as-release-builds-and-fix-checksum-snippet.md:
# the tag build in .github/workflows/ci.yml must stamp RawBuildKind=release,
# and a build without BUILD_KIND must stay "local" (Makefile default).
#
# It checks the command and its expansion (make -n) and, where the host can run
# the result, one built binary's behaviour (see the probe below). The binary
# does not expose its build kind. It builds into a temporary directory only.
#
#   sh scripts/check_release_flags.sh [ROOT]
set -eu

ROOT=${1:-$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)}
WF="$ROOT/.github/workflows/ci.yml"
FAIL=0
bad() { printf 'FAIL %s\n' "$1" >&2; FAIL=1; }
ok() { printf 'ok   %s\n' "$1"; }

[ -f "$WF" ] || { echo "FAIL missing $WF" >&2; exit 1; }

# The tag build's command, as written in the workflow.
LINES=$(grep -E '^[[:space:]]*make build-all' "$WF" || true)
N=$(printf '%s' "$LINES" | grep -c . || true)
if [ "$N" -ne 1 ]; then
    bad "expected exactly one 'make build-all' line in ci.yml, found $N"
    exit 1
fi
CMD=$(printf '%s\n' "$LINES" | sed 's/^[[:space:]]*//')

case "$CMD" in
*BUILD_KIND=release*) ok "ci.yml's tag build passes BUILD_KIND=release: $CMD" ;;
*) bad "ci.yml's tag build does not pass BUILD_KIND=release: $CMD" ;;
esac

count() { # $1 = text, $2 = pattern
    printf '%s\n' "$1" | grep -c -- "$2" || true
}

# Expansion of the workflow's own command (TAG is the workflow's variable).
OUT=$(cd "$ROOT" && TAG=v0.0.0-test sh -c "$CMD -n" 2>&1) || { bad "make -n failed: $OUT"; exit 1; }
BUILDS=$(count "$OUT" 'go build')
REL=$(count "$OUT" 'main.RawBuildKind=release')
LOC=$(count "$OUT" 'main.RawBuildKind=local')
if [ "$BUILDS" -eq 4 ] && [ "$REL" -eq 4 ] && [ "$LOC" -eq 0 ]; then
    ok "the workflow's command stamps release on all 4 targets"
else
    bad "workflow command expands to $BUILDS builds, $REL release, $LOC local (want 4, 4, 0)"
fi

# The Makefile's own default must stay local (no inference from VERSION).
DEF=$(cd "$ROOT" && make -n build-all VERSION=v1.2.3 2>&1) || { bad "make -n (default) failed"; exit 1; }
DREL=$(count "$DEF" 'main.RawBuildKind=release')
DLOC=$(count "$DEF" 'main.RawBuildKind=local')
if [ "$DREL" -eq 0 ] && [ "$DLOC" -eq 4 ]; then
    ok "a tag-shaped VERSION without BUILD_KIND stays local on all 4 targets"
else
    bad "default build expands to $DREL release, $DLOC local (want 0, 4)"
fi

# Runtime probe of a produced binary, with no user-facing surface: a release
# build validates its own version before any network call, so a release-stamped
# binary whose version is not vMAJOR.MINOR.PATCH fails `update --check` at
# once ("is not a strict stable tag"); a local-stamped one goes on to the
# network. Runs only where this host can run a binary the Makefile builds.
case "$(uname -s)-$(uname -m)" in
Linux-x86_64) TARGET=linux; BIN=mcp-server-recall-linux-amd64 ;;
Darwin-arm64) TARGET=darwin-arm64; BIN=mcp-server-recall-darwin-arm64 ;;
*) TARGET=; BIN= ;;
esac
if [ -n "$TARGET" ] && command -v go >/dev/null 2>&1; then
    TMP=$(mktemp -d)
    trap 'rm -rf "$TMP"' EXIT INT TERM
    if (cd "$ROOT" && make "$TARGET" VERSION=v0.0.0-test BUILD_KIND=release DIST_DIR="$TMP/dist") >"$TMP/build.log" 2>&1; then
        mkdir -p "$TMP/home"
        set +e
        PROBE=$(env -i HOME="$TMP/home" PATH="/usr/bin:/bin" TMPDIR="$TMP" \
            "$TMP/dist/$BIN" update --check 2>&1)
        RC=$?
        set -e
        case "$PROBE" in
        *"is not a strict stable tag"*)
            if [ "$RC" -eq 1 ]; then
                ok "built $BIN is a release build (update --check fails version validation first)"
            else
                bad "probe exit $RC, want 1"
            fi ;;
        *) bad "built $BIN is not a release build: update --check did not fail version validation (exit $RC)" ;;
        esac
    else
        bad "could not build $TARGET: $(tail -3 "$TMP/build.log")"
    fi
else
    echo "skip runtime probe: no buildable native target on this host"
fi

[ "$FAIL" -eq 0 ] && echo "release build flags: pass"
exit "$FAIL"
