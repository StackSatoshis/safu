#!/bin/sh
# safu.sh installer
# Usage:
#   curl -fsSL https://safu.sh/install.sh | sh
#
# Environment variables:
#   VERSION       - specific version to install (default: latest, e.g. VERSION=v0.2.1)
#   INSTALL_DIR   - install location (default: /usr/local/bin, or ~/.local/bin if no sudo)
#   SAFU_NO_VERIFY - set to skip checksum verification (not recommended)

set -eu

REPO="StackSatoshis/safu"
BINARY="safu"
GITHUB_BASE="https://github.com/${REPO}/releases"
# CDN mirror (optional - falls back to GitHub if unavailable)
CDN_BASE="https://cdn.safu.sh"

# --------- Pretty output ---------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    BOLD="$(printf '\033[1m')"
    DIM="$(printf '\033[2m')"
    GREEN="$(printf '\033[32m')"
    YELLOW="$(printf '\033[33m')"
    RED="$(printf '\033[31m')"
    RESET="$(printf '\033[0m')"
else
    BOLD=""; DIM=""; GREEN=""; YELLOW=""; RED=""; RESET=""
fi

info()  { printf "%s==>%s %s\n" "$BOLD" "$RESET" "$1"; }
ok()    { printf "%s✓%s %s\n" "$GREEN" "$RESET" "$1"; }
warn()  { printf "%s!%s %s\n" "$YELLOW" "$RESET" "$1"; }
err()   { printf "%s✗%s %s\n" "$RED" "$RESET" "$1" >&2; }
die()   { err "$1"; exit 1; }

# --------- Pre-flight ---------
need() {
    command -v "$1" >/dev/null 2>&1 || die "required tool not found: $1"
}
need curl
need tar
need uname

# --------- Detect OS ---------
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)      die "unsupported OS: $OS (only linux and macOS for now — windows coming soon)" ;;
esac

# --------- Detect arch ---------
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)             die "unsupported architecture: $ARCH" ;;
esac

info "detected ${BOLD}${OS}/${ARCH}${RESET}"

# --------- Resolve version ---------
VERSION="${VERSION:-latest}"
if [ "$VERSION" = "latest" ]; then
    info "resolving latest release..."
    VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name":' \
        | head -1 \
        | sed -E 's/.*"([^"]+)".*/\1/')"
    [ -z "$VERSION" ] && die "could not resolve latest version"
fi

# Normalize: GoReleaser archives are usually named without the leading 'v' in the version part
VERSION_NUM="${VERSION#v}"
ARCHIVE="${BINARY}_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"

info "installing ${BOLD}${BINARY} ${VERSION}${RESET}"

# --------- Download ---------
TMP="$(mktemp -d 2>/dev/null || mktemp -d -t safu)"
trap 'rm -rf "$TMP"' EXIT INT TERM

CDN_URL="${CDN_BASE}/${VERSION}/${ARCHIVE}"
GH_URL="${GITHUB_BASE}/download/${VERSION}/${ARCHIVE}"

info "downloading ${DIM}${ARCHIVE}${RESET}"
if curl -fsSL --connect-timeout 5 "$CDN_URL" -o "${TMP}/${ARCHIVE}" 2>/dev/null; then
    ok "fetched from cdn"
else
    warn "cdn miss — falling back to github"
    curl -fsSL "$GH_URL" -o "${TMP}/${ARCHIVE}" \
        || die "download failed: $GH_URL"
    ok "fetched from github"
fi

# --------- Verify checksum (always from GitHub for security) ---------
if [ -z "${SAFU_NO_VERIFY:-}" ]; then
    info "verifying checksum"
    curl -fsSL "${GITHUB_BASE}/download/${VERSION}/checksums.txt" -o "${TMP}/checksums.txt" \
        || die "could not fetch checksums.txt"

    EXPECTED="$(grep "  ${ARCHIVE}\$" "${TMP}/checksums.txt" | awk '{print $1}')"
    [ -z "$EXPECTED" ] && die "no checksum entry for ${ARCHIVE}"

    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL="$(sha256sum "${TMP}/${ARCHIVE}" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        ACTUAL="$(shasum -a 256 "${TMP}/${ARCHIVE}" | awk '{print $1}')"
    else
        die "no sha256 tool found (need sha256sum or shasum)"
    fi

    [ "$EXPECTED" = "$ACTUAL" ] || die "checksum mismatch — refusing to install"
    ok "checksum verified"
else
    warn "skipping checksum verification (SAFU_NO_VERIFY set)"
fi

# --------- Extract ---------
info "extracting"
tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP" \
    || die "extraction failed"

[ -f "${TMP}/${BINARY}" ] || die "binary not found in archive"
chmod +x "${TMP}/${BINARY}"

# --------- Choose install dir ---------
if [ -z "${INSTALL_DIR:-}" ]; then
    if [ -w "/usr/local/bin" ]; then
        INSTALL_DIR="/usr/local/bin"
    elif [ "$(id -u)" = "0" ]; then
        INSTALL_DIR="/usr/local/bin"
    elif command -v sudo >/dev/null 2>&1; then
        INSTALL_DIR="/usr/local/bin"
    else
        INSTALL_DIR="${HOME}/.local/bin"
        mkdir -p "$INSTALL_DIR"
    fi
fi

DEST="${INSTALL_DIR}/${BINARY}"

# --------- Install ---------
if [ -w "$INSTALL_DIR" ]; then
    mv "${TMP}/${BINARY}" "$DEST"
else
    info "installing to ${INSTALL_DIR} (requires sudo)"
    sudo mv "${TMP}/${BINARY}" "$DEST"
fi

ok "installed ${BOLD}${BINARY}${RESET} to ${DEST}"

# --------- PATH check ---------
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        warn "${INSTALL_DIR} is not on your PATH"
        printf "  add this to your shell profile:\n"
        printf "    %sexport PATH=\"%s:\$PATH\"%s\n" "$DIM" "$INSTALL_DIR" "$RESET"
        ;;
esac

# --------- Smoke test ---------
if "$DEST" --version >/dev/null 2>&1; then
    VER_OUTPUT="$("$DEST" --version 2>/dev/null | head -1)"
    ok "${VER_OUTPUT}"
fi

# --------- Offer to enable the shell integration ---------
# Installing only places the binary. We never SILENTLY modify your shell — we
# ask first, then run `safu init --write-rc` (which makes a timestamped backup).
# Set SAFU_NO_SETUP=1 to skip this prompt in scripted installs.
if [ -c /dev/tty ] && [ -z "${SAFU_NO_SETUP:-}" ]; then
    printf "\n%senable safu in your shell now?%s adds the hook to your shell rc, backup made first [Y/n] " \
        "$BOLD" "$RESET" > /dev/tty
    read SAFU_ANS < /dev/tty || SAFU_ANS=""
    case "$SAFU_ANS" in
        ""|y|Y|yes|YES)
            if "$DEST" init --write-rc > /dev/tty 2>&1; then
                ok "shell integration enabled — restart your shell (or open a new tab) to activate"
            else
                warn "couldn't enable automatically — run ${BOLD}safu init --write-rc${RESET} yourself"
            fi
            ;;
        *)
            info "skipped — enable later with ${BOLD}safu setup${RESET} (or ${BOLD}safu init --write-rc${RESET})"
            ;;
    esac
else
    info "enable the shell integration with ${BOLD}safu setup${RESET} (or ${BOLD}safu init --write-rc${RESET})"
fi

printf "\n"
printf "  ${BOLD}done.${RESET} try ${GREEN}${BINARY} --help${RESET} to get started.\n"
printf "  docs: ${DIM}https://safu.sh${RESET}\n\n"
