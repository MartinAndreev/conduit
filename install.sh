#!/usr/bin/env sh
set -eu

# The release workflow replaces this placeholder. For forks or self-hosted
# releases, set CONDUIT_REPOSITORY=owner/repository before running the script.
REPOSITORY="${CONDUIT_REPOSITORY:-__CONDUIT_REPOSITORY__}"
VERSION="${CONDUIT_VERSION:-latest}"
INSTALL_DIR="${CONDUIT_INSTALL_DIR:-${XDG_BIN_HOME:-$HOME/.local/bin}}"

if [ "$REPOSITORY" = "__CONDUIT_REPOSITORY__" ]; then
  echo "Set CONDUIT_REPOSITORY=owner/repository before running this installer." >&2
  exit 1
fi

os="$(uname -s)"
arch="$(uname -m)"
case "$os/$arch" in
  Linux/x86_64) asset="conduit-linux-x64" ;;
  Darwin/x86_64) asset="conduit-darwin-x64" ;;
  Darwin/arm64) asset="conduit-darwin-arm64" ;;
  *) echo "Unsupported platform: $os/$arch" >&2; exit 1 ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  echo "curl or wget is required." >&2
  exit 1
fi

base="https://github.com/$REPOSITORY/releases"
if [ "$VERSION" = "latest" ]; then
  base="$base/latest/download"
else
  base="$base/download/$VERSION"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
fetch "$base/$asset" "$tmp/$asset"
fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS"

expected="$(awk -v name="$asset" '$2 == name { print $1 }' "$tmp/SHA256SUMS")"
if [ -z "$expected" ]; then
  echo "No checksum found for $asset." >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
fi
if [ "$expected" != "$actual" ]; then
  echo "Checksum verification failed for $asset." >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
install -m 755 "$tmp/$asset" "$INSTALL_DIR/conduit"
echo "Installed Conduit to $INSTALL_DIR/conduit"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "Add $INSTALL_DIR to PATH, then open a new terminal." ;;
esac
