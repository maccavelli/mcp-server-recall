#!/bin/sh
# mcp-server-recall bootstrap installer — docs/0003-MADR-curl-bootstrap-installer.md
#
#   curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh | sh
#
# Properties this script must keep (magic-cli-remote 0097/0104/0116):
#   * POSIX sh (Alpine /bin/sh is busybox ash). Verify: shellcheck -s sh
#   * never invokes sudo — everything lands under $HOME
#   * no GitHub API call
#   * verifies SHA-256 by VALUE, not filename
#   * the whole body lives in main(), invoked on the last line
#
# Exit codes: 0 ok  1 usage/unsupported  2 download or verify failed
set -eu

REPO_URL="https://github.com/maccavelli/mcp-server-recall/releases"
PRODUCT="mcp-server-recall"
TMP_DIR=""

log()  { printf '%s\n' "$*" >&2; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$2" >&2; exit "$1"; }
vlog() { [ "${VERBOSE:-0}" = 1 ] && printf '  %s\n' "$*" >&2 || true; }

# shellcheck disable=SC2329  # invoked indirectly via trap
cleanup() { [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && rm -rf "$TMP_DIR" || true; }

have() { command -v "$1" >/dev/null 2>&1; }

fetch() {
    case "$1" in
        /*) [ -f "$1" ] || return 1; cp -f "$1" "$2" ;;
        *)
            if have curl; then
                curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
            else
                wget -q -O "$2" "$1"
            fi
            ;;
    esac
}

sha256_of() {
    if have sha256sum; then
        sha256sum "$1" | awk '{print $1}'
    elif have shasum; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        openssl dgst -sha256 "$1" | awk '{print $NF}'
    fi
}

detect_arch() {
    uname_s=$(uname -s)
    case "$uname_s" in
        Linux)  OS=linux ;;
        Darwin) OS=darwin ;;
        *)
            die 1 "this installer supports Linux and macOS only (found $uname_s).
Windows: irm https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.ps1 | iex"
            ;;
    esac

    uname_m=$(uname -m)
    case "$uname_m" in
        x86_64|amd64)   ARCH=amd64 ;;
        aarch64|arm64)  ARCH=arm64 ;;
        armv6l|armv7l|armhf)
            die 1 "32-bit ARM ($uname_m) is not published; only amd64 and arm64 are built." ;;
        *)
            die 1 "unsupported architecture $uname_m; only amd64 and arm64 are published." ;;
    esac

    if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ]; then
        die 1 "darwin/amd64 is not a published target.
Intel Macs have no supported current build."
    fi
}

verify_and_resolve() {
    _line=$(grep -E "  ${PRODUCT}-${OS}-${ARCH}-[0-9]" "$TMP_DIR/SHA256SUMS" | head -n 1) || true
    [ -n "$_line" ] || die 2 "no checksum entry for ${PRODUCT}-${OS}-${ARCH} in SHA256SUMS"

    _want=$(printf '%s\n' "$_line" | awk '{print $1}')
    _name=$(printf '%s\n' "$_line" | awk '{print $NF}')
    _got=$(sha256_of "$TMP_DIR/$PRODUCT")

    if [ "$_want" != "$_got" ]; then
        die 2 "checksum mismatch for $PRODUCT
  expected $_want
  got      $_got
Nothing was installed."
    fi
    RESOLVED_VER=${_name#"${PRODUCT}-${OS}-${ARCH}-"}
    RESOLVED_VER=${RESOLVED_VER%.exe}
    vlog "$PRODUCT verified, version $RESOLVED_VER"
}

download_all() {
    if [ -n "${PIN_VERSION:-}" ]; then
        URL_DIR="$BASE_URL/download/v$PIN_VERSION"
    else
        URL_DIR="$BASE_URL/latest/download"
    fi
    vlog "source $URL_DIR"

    fetch "$URL_DIR/SHA256SUMS" "$TMP_DIR/SHA256SUMS" ||
        die 2 "could not download SHA256SUMS from $URL_DIR
If you pinned a version, check that the release exists and carries the
unversioned alias assets."

    fetch "$URL_DIR/${PRODUCT}-${OS}-${ARCH}" "$TMP_DIR/$PRODUCT" ||
        die 2 "could not download ${PRODUCT}-${OS}-${ARCH} from $URL_DIR"
    verify_and_resolve
}

install_binary() {
    mkdir -p "$INSTALL_DIR" || die 1 "cannot create $INSTALL_DIR"
    chmod 0755 "$TMP_DIR/$PRODUCT"
    _target="$INSTALL_DIR/$PRODUCT"
    # Rename a pre-existing binary aside first. Overwriting a running Darwin
    # Mach-O in place hits ETXTBSY; a rename of the directory entry succeeds
    # and the process keeps its old inode.
    if [ -e "$_target" ]; then
        rm -f "${_target}.prev"
        mv -f "$_target" "${_target}.prev" || die 2 "cannot move aside $_target"
    fi
    mv -f "$TMP_DIR/$PRODUCT" "$_target" || die 2 "cannot install $PRODUCT to $INSTALL_DIR"
    log "installed $_target"

    _reported=$("$_target" --version 2>/dev/null | awk '{print $NF}') || _reported=""
    if [ -n "$_reported" ] && [ -n "${RESOLVED_VER:-}" ] && [ "$_reported" != "$RESOLVED_VER" ]; then
        warn "installed binary reports '$_reported' but the manifest said '$RESOLVED_VER'"
    fi
}

clear_quarantine() {
    have xattr || return 0
    [ -f "$INSTALL_DIR/$PRODUCT" ] || return 0
    xattr -d com.apple.quarantine "$INSTALL_DIR/$PRODUCT" 2>/dev/null || true
}

check_path() {
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            log ""
            log "note: $INSTALL_DIR is not on your PATH. Add it with:"
            log "    export PATH=\"\$PATH:$INSTALL_DIR\""
            ;;
    esac
}

do_uninstall() {
    _t="$INSTALL_DIR/$PRODUCT"
    if [ -e "$_t" ]; then
        rm -f "$_t" && log "removed $_t"
    else
        log "nothing to remove at $_t"
    fi
    rm -f "${_t}.prev" 2>/dev/null || true
}

usage() {
    cat >&2 <<'EOF'
mcp-server-recall Linux / macOS installer

  install.sh [--version X.Y.Z] [--dir PATH] [--dry-run] [--verbose]
             [--uninstall] [--help]

Piped invocation cannot take flags after `| sh`, so use the environment
equivalents instead:

  MCP_RECALL_VERSION      same as --version
  MCP_RECALL_INSTALL_DIR  same as --dir           (default ~/.local/bin)

  curl -fsSL <url>/install.sh | MCP_RECALL_VERSION=1.0.3 sh
EOF
}

main() {
    INSTALL_DIR="${MCP_RECALL_INSTALL_DIR:-$HOME/.local/bin}"
    PIN_VERSION="${MCP_RECALL_VERSION:-}"
    BASE_URL="${MC_TEST_BASE_URL:-$REPO_URL}"
    DRY_RUN=0; VERBOSE=0; UNINSTALL=0
    RESOLVED_VER=""

    while [ $# -gt 0 ]; do
        case "$1" in
            --version) PIN_VERSION="${2:?--version needs a value}"; shift 2 ;;
            --dir)     INSTALL_DIR="${2:?--dir needs a value}"; shift 2 ;;
            --dry-run)            DRY_RUN=1; shift ;;
            --verbose|-v)         VERBOSE=1; shift ;;
            --uninstall)          UNINSTALL=1; shift ;;
            --help|-h)            usage; exit 0 ;;
            *) usage; die 1 "unknown option: $1" ;;
        esac
    done

    PIN_VERSION="${PIN_VERSION#v}"

    detect_arch

    if [ "$UNINSTALL" = 1 ]; then
        do_uninstall
        exit 0
    fi

    vlog "os=$OS arch=$ARCH"

    missing=""
    case "$BASE_URL" in
        /*) ;;
        *) have curl || have wget || missing="$missing curl-or-wget" ;;
    esac
    have sha256sum || have shasum || have openssl || missing="$missing sha256sum-or-shasum-or-openssl"
    have awk  || missing="$missing awk"
    have grep || missing="$missing grep"
    [ -z "$missing" ] || die 1 "missing required tools:$missing"

    if [ "$DRY_RUN" = 1 ]; then
        log "dry run — nothing will be written"
        log "  os:          $OS"
        log "  arch:        $ARCH"
        log "  install dir: $INSTALL_DIR"
        if [ -n "$PIN_VERSION" ]; then
            log "  source:      $BASE_URL/download/v$PIN_VERSION"
        else
            log "  source:      $BASE_URL/latest/download"
        fi
        exit 0
    fi

    mkdir -p "$INSTALL_DIR" || die 1 "cannot create $INSTALL_DIR"
    INSTALL_DIR=$(CDPATH='' cd -- "$INSTALL_DIR" && pwd) ||
        die 1 "cannot resolve $INSTALL_DIR"
    TMP_DIR=$(mktemp -d "$INSTALL_DIR/.recall-install.XXXXXX") || die 1 "cannot create a temp dir in $INSTALL_DIR"
    trap cleanup EXIT INT TERM

    download_all
    install_binary
    clear_quarantine
    check_path

    log ""
    log "$PRODUCT ${RESOLVED_VER:-unknown} installed to $INSTALL_DIR"
    log "next:    $INSTALL_DIR/$PRODUCT configure"
    exit 0
}

main "$@"
