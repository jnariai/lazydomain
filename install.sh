#!/bin/sh
set -eu

REPO="jnariai/lazydomain"
VERSION="${LAZYDOMAIN_VERSION:-latest}"
INSTALL_DIR="${LAZYDOMAIN_INSTALL_DIR:-$HOME/.local/bin}"
GITHUB_URL="${LAZYDOMAIN_GITHUB_URL:-https://github.com}"
RELEASES_URL="$GITHUB_URL/$REPO/releases"

say() { printf '%s\n' "$*"; }
die() { printf 'lazydomain install: %s\n' "$*" >&2; exit 1; }

case "${1:-}" in
  "") ;;
  --uninstall)
    shift
    bin="$INSTALL_DIR/lazydomain"
    [ -x "$bin" ] || bin=$(command -v lazydomain 2>/dev/null || true)
    [ -n "$bin" ] && [ -x "$bin" ] || die "lazydomain is not installed in $INSTALL_DIR or on your PATH"
    # When piped from curl, stdin is this script; reattach the terminal so
    # the confirmation prompt, sudo and UAC messages work.
    # The probe runs in a subshell: in POSIX sh a failed redirection on a
    # special builtin would otherwise exit this script.
    if [ -t 0 ] || ! (exec </dev/tty) 2>/dev/null; then
      exec "$bin" uninstall "$@"
    fi
    exec "$bin" uninstall "$@" </dev/tty
    ;;
  *) die "unknown option '$1' (supported: --uninstall)" ;;
esac

need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed"; }
need curl
need tar
need uname
need install
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "sha256sum or shasum is required to verify the download"
fi

os=$(uname -s)
[ "$os" = "Linux" ] || die "lazydomain supports Linux and WSL2 only (detected $os)"

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported CPU architecture: $(uname -m)" ;;
esac

if [ "$VERSION" = "latest" ]; then
  # /releases/latest redirects to /releases/tag/<tag>; read the tag from the final URL.
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$RELEASES_URL/latest") ||
    die "could not reach GitHub to find the latest release"
  tag=${url##*/}
  case "$tag" in
    v*) ;;
    *) die "no published release found for $REPO" ;;
  esac
else
  tag=$VERSION
  case "$tag" in v*) ;; *) tag="v$tag" ;; esac
fi

version=${tag#v}
asset="lazydomain_${version}_linux_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading lazydomain $tag (linux/$arch)…"
curl -fsSL -o "$tmp/$asset" "$RELEASES_URL/download/$tag/$asset" ||
  die "download failed: $RELEASES_URL/download/$tag/$asset (does release $tag exist?)"
curl -fsSL -o "$tmp/checksums.txt" "$RELEASES_URL/download/$tag/checksums.txt" ||
  die "download failed: checksums.txt for $tag"

expected=$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || die "$asset is not listed in checksums.txt"
actual=$(sha256 "$tmp/$asset")
[ "$expected" = "$actual" ] || die "checksum mismatch for $asset (expected $expected, got $actual)"

tar -xzf "$tmp/$asset" -C "$tmp" lazydomain
install -d "$INSTALL_DIR"
install -m 0755 "$tmp/lazydomain" "$INSTALL_DIR/lazydomain"

say "✓ Installed $("$INSTALL_DIR/lazydomain" version) to $INSTALL_DIR/lazydomain"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    say ""
    say "! $INSTALL_DIR is not on your PATH. Add this to your ~/.bashrc or ~/.zshrc:"
    say "    export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac

say ""
say "First time? Run once:"
say "    lazydomain proxy up   # start the shared HTTPS proxy (Docker)"
say "    lazydomain trust      # trust its certificate in Windows and WSL"
say "Then just run: lazydomain"
