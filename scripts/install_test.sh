#!/bin/sh
# Tests for scripts/install.sh (docs/0003-MADR-curl-bootstrap-installer.md).
#
# Fully offline: a fake release directory is served via MC_TEST_BASE_URL, and
# PATH is replaced with a stub directory whose `uname` reports Linux by default
# (Darwin when a case overwrites it).
#
#   sh scripts/install_test.sh
set -eu

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
INSTALLER="$HERE/install.sh"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; [ $# -gt 1 ] && printf '       %s\n' "$2"; }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "want [$3] got [$2]"; fi; }
contains() { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1" "missing [$3] in: $2" ;; esac; }

sha_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
    else shasum -a 256 "$1" | awk '{print $1}'; fi
}

VER=9.9.9
PRODUCT=mcp-server-recall

mk_release() { # $1 = dir, $2 = arch, $3 = os (default linux), $4 = "corrupt"
    _os=${3:-linux}
    _corrupt=${4:-}
    case "${3:-}" in corrupt) _os=linux; _corrupt=corrupt ;; esac
    rel="$1/latest/download"; mkdir -p "$rel"
    printf '#!/bin/sh\nif [ "$1" = configure ]; then printf "%%s\\n" "$*" > "$(dirname "$0")/.configure-args"; exit 0; fi\necho "%s version %s"\n' "$PRODUCT" "$VER" > "$rel/$PRODUCT-$_os-$2"
    chmod 0755 "$rel/$PRODUCT-$_os-$2"
    : > "$rel/SHA256SUMS"
    printf '%s  %s-%s-%s-%s\n' "$(sha_of "$rel/$PRODUCT-$_os-$2")" "$PRODUCT" "$_os" "$2" "$VER" >> "$rel/SHA256SUMS"
    if [ "$_corrupt" = corrupt ]; then
        printf '#!/bin/sh\necho tampered\n' > "$rel/$PRODUCT-$_os-$2"
        chmod 0755 "$rel/$PRODUCT-$_os-$2"
    fi
}

mk_stubs() { # $1 = dir, $2 = uname -m value
    d="$1"; um="$2"; mkdir -p "$d"
    for t in sh mktemp mkdir mv rm chmod cat grep awk cp id tail head find wc tr sed sleep basename dirname; do
        p=$(command -v "$t" 2>/dev/null) || continue
        [ -e "$d/$t" ] || ln -sf "$p" "$d/$t"
    done
    for t in sha256sum shasum openssl; do
        p=$(command -v "$t" 2>/dev/null) || continue
        [ -e "$d/$t" ] || ln -sf "$p" "$d/$t"
    done
    printf '#!/bin/sh\ncase "$1" in -m) echo %s ;; -s) echo Linux ;; *) echo Linux ;; esac\n' \
        "$um" > "$d/uname"
    chmod 0755 "$d/uname"
}

run_installer() {
    _rp="$1"; _url="$2"; _dir="$3"; shift 3
    ( set +e
      [ -n "$_rp" ] && { PATH="$_rp"; export PATH; }
      [ "$_url" != "-" ] && { MC_TEST_BASE_URL="$_url"; export MC_TEST_BASE_URL; }
      MCP_RECALL_INSTALL_DIR="$_dir"; export MCP_RECALL_INSTALL_DIR
      "$INSTALLER" "$@" >"$WORK/out" 2>"$WORK/err"
      echo $? > "$WORK/rc" )
    RC=$(cat "$WORK/rc"); OUT=$(cat "$WORK/out" "$WORK/err" 2>/dev/null || true)
}

printf '\n1-2. architecture mapping\n'
for pair in "x86_64 amd64" "amd64 amd64" "aarch64 arm64" "arm64 arm64"; do
    m=${pair% *}; want=${pair#* }
    S="$WORK/stub-arch-$m"; mk_stubs "$S" "$m"
    R="$WORK/rel-$want"; [ -d "$R" ] || mk_release "$R" "$want"
    run_installer "$S" "$R" "$WORK/bin-arch-$m" --dry-run --verbose
    check "uname -m=$m maps to $want" "$(printf '%s\n' "$OUT" | grep -c "arch: *$want")" 1
    contains "  uname -m=$m reports os: linux" "$OUT" "os:          linux"
    contains "  uname -m=$m default encrypt-db true" "$OUT" "configure:   --encrypt-db=true"
done

for m in armv7l armv6l armhf; do
    S="$WORK/stub-$m"; mk_stubs "$S" "$m"
    run_installer "$S" - "$WORK/bin-$m" --dry-run
    check "$m rejected (exit 1)" "$RC" 1
    contains "  $m message names 32-bit ARM" "$OUT" "32-bit ARM"
done

for m in i686 riscv64; do
    S="$WORK/stub-$m"; mk_stubs "$S" "$m"
    run_installer "$S" - "$WORK/bin-$m" --dry-run
    check "$m rejected (exit 1)" "$RC" 1
    contains "  $m message names the arch" "$OUT" "unsupported architecture"
done

printf '\n3. Darwin arm64 accepted / others rejected\n'
S="$WORK/stub-darwin"; mk_stubs "$S" arm64
printf '#!/bin/sh\ncase "$1" in -s) echo Darwin ;; -m) echo arm64 ;; esac\n' > "$S/uname"
chmod 0755 "$S/uname"
R="$WORK/rel-darwin-arm64"; mk_release "$R" arm64 darwin
run_installer "$S" "$R" "$WORK/bin-darwin" --dry-run --verbose
check "Darwin arm64 accepted (exit 0)" "$RC" 0
contains "  Darwin dry-run reports os: darwin" "$OUT" "os:          darwin"
contains "  Darwin dry-run reports arch: arm64" "$OUT" "arch:        arm64"

S="$WORK/stub-darwin-amd64"; mk_stubs "$S" x86_64
printf '#!/bin/sh\ncase "$1" in -s) echo Darwin ;; -m) echo x86_64 ;; esac\n' > "$S/uname"
chmod 0755 "$S/uname"
run_installer "$S" - "$WORK/bin-darwin-amd64" --dry-run --verbose
check "Darwin x86_64 rejected (exit 1)" "$RC" 1
contains "  Darwin x86_64 message names unpublished target" "$OUT" "darwin/amd64 is not a published target"

S="$WORK/stub-freebsd"; mk_stubs "$S" amd64
printf '#!/bin/sh\ncase "$1" in -s) echo FreeBSD ;; -m) echo amd64 ;; esac\n' > "$S/uname"
chmod 0755 "$S/uname"
run_installer "$S" - "$WORK/bin-freebsd" --dry-run
check "FreeBSD rejected (exit 1)" "$RC" 1
contains "  FreeBSD message names Linux and macOS" "$OUT" "Linux and macOS only"

S="$WORK/stub-darwin-corrupt"; mk_stubs "$S" arm64
printf '#!/bin/sh\ncase "$1" in -s) echo Darwin ;; -m) echo arm64 ;; esac\n' > "$S/uname"
chmod 0755 "$S/uname"
R="$WORK/rel-darwin-corrupt"; mk_release "$R" arm64 darwin corrupt
D="$WORK/bin-darwin-corrupt"; mkdir -p "$D"; printf 'PREEXISTING\n' > "$D/$PRODUCT"
run_installer "$S" "$R" "$D"
check "Darwin checksum mismatch exits 2" "$RC" 2
check "  Darwin existing install untouched" "$(cat "$D/$PRODUCT")" "PREEXISTING"

ARCH=amd64
BASE="$WORK/stub-base"; mk_stubs "$BASE" x86_64

printf '\n4-7. download and verification\n'

R="$WORK/rel-nomatch"; mk_release "$R" "$ARCH"
: > "$R/latest/download/SHA256SUMS"
D="$WORK/bin-nomatch"
run_installer "$BASE" "$R" "$D"
check "missing manifest entry exits 2" "$RC" 2
check "  nothing installed" "$( [ -e "$D/$PRODUCT" ] && echo yes || echo no )" no

R="$WORK/rel-corrupt"; mk_release "$R" "$ARCH" linux corrupt
D="$WORK/bin-corrupt"; mkdir -p "$D"; printf 'PREEXISTING\n' > "$D/$PRODUCT"
run_installer "$BASE" "$R" "$D"
check "checksum mismatch exits 2" "$RC" 2
check "  existing install untouched" "$(cat "$D/$PRODUCT")" "PREEXISTING"
check "  no temp dir left behind" "$(find "$D" -maxdepth 1 -name '.recall-install.*' | wc -l | tr -d ' ')" 0

R="$WORK/rel-ok"; mk_release "$R" "$ARCH"; D="$WORK/bin-ok"
run_installer "$BASE" "$R" "$D"
check "valid digest installs (exit 0)" "$RC" 0
check "  binary installed executable" "$( [ -x "$D/$PRODUCT" ] && echo yes || echo no )" yes
contains "  resolved version reported" "$OUT" "$VER"

# A manifest carrying BOTH shapes is ambiguous and must fail closed. Before
# MADR 0005 the versioned line simply won, but once canonical entries are
# legitimate a preference either way lets an appended line authorize a
# substituted binary. Refusing to choose is the only safe answer.
R="$WORK/rel-alias"; mk_release "$R" "$ARCH"
printf 'deadbeef  %s-linux-%s\n' "$PRODUCT" "$ARCH" >> "$R/latest/download/SHA256SUMS"
D="$WORK/bin-alias"; mkdir -p "$D"; printf 'PREEXISTING\n' > "$D/$PRODUCT"
run_installer "$BASE" "$R" "$D"
check "ambiguous manifest (canonical + versioned) exits 2" "$RC" 2
check "  existing install untouched by an ambiguous manifest" "$(cat "$D/$PRODUCT")" "PREEXISTING"

# A canonical-only manifest is the v-next shape and must install cleanly.
R="$WORK/rel-canonical"; mk_release "$R" "$ARCH"
printf '%s  %s-linux-%s\n' "$(sha_of "$R/latest/download/$PRODUCT-linux-$ARCH")" "$PRODUCT" "$ARCH" \
    > "$R/latest/download/SHA256SUMS"
D="$WORK/bin-canonical"
run_installer "$BASE" "$R" "$D"
check "canonical-only manifest installs (exit 0)" "$RC" 0
check "  binary installed executable" "$( [ -x "$D/$PRODUCT" ] && echo yes || echo no )" yes

# The remaining cases need a conforming release, not the tampered one.
R="$WORK/rel-rerun"; mk_release "$R" "$ARCH"
D="$WORK/bin-rerun"
run_installer "$BASE" "$R" "$D"
check "first install for the re-run case (exit 0)" "$RC" 0

run_installer "$BASE" "$R" "$D"
check "re-run is idempotent (exit 0)" "$RC" 0
check "  no temp dir left behind" "$(find "$D" -maxdepth 1 -name '.recall-install.*' | wc -l | tr -d ' ')" 0
check "  previous binary kept as .prev" "$( [ -e "$D/${PRODUCT}.prev" ] && echo yes || echo no )" yes
check "  configure invoked with encrypt-db=true" "$(cat "$D/.configure-args" 2>/dev/null || echo missing)" "configure --encrypt-db=true"

D="$WORK/bin-no-cfg"
run_installer "$BASE" "$R" "$D" --no-configure
check "--no-configure does not invoke configure" "$( [ -e "$D/.configure-args" ] && echo yes || echo no )" no

run_installer "$BASE" "$R" "$WORK/bin-enc-false" --encrypt-db=false
check "--encrypt-db=false passed to configure" "$(cat "$WORK/bin-enc-false/.configure-args" 2>/dev/null || echo missing)" "configure --encrypt-db=false"

run_installer "$BASE" - "$WORK/bin-bad-enc" --encrypt-db=maybe --dry-run
check "invalid --encrypt-db exits 1" "$RC" 1

last=$(tail -n 1 "$INSTALLER")
check "script ends with main \"\$@\"" "$last" 'main "$@"'

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
