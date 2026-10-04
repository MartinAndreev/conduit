#!/bin/sh
set -eu
fail() { echo "$*" >&2; exit 1; }
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
command -v go >/dev/null 2>&1 || fail "Go is required. Install Go and add it to PATH."
tags=desktop,production
case $(uname -s) in
    Darwin)
        command -v xcrun >/dev/null 2>&1 || fail "GUI requires Xcode command line tools (xcrun)."
        if [ -z "${SDKROOT:-}" ]; then
            selected_sdk=$(xcrun --show-sdk-path) || fail "Cannot locate the selected macOS SDK."
            developer_dir=$(xcode-select -p) || fail "Cannot locate Xcode developer tools."
            # SDK 27 can exceed the selected linker's TAPI support. Prefer installed 26.5.
            for sdk in "$developer_dir/Platforms/MacOSX.platform/Developer/SDKs/MacOSX26.5.sdk" "/Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX26.5.sdk" "$(dirname "$selected_sdk")/MacOSX26.5.sdk"; do
                if [ -d "$sdk" ]; then selected_sdk=$sdk; break; fi
            done
            SDKROOT=$selected_sdk
            export SDKROOT
        fi
        [ -d "$SDKROOT" ] || fail "macOS SDKROOT does not exist: $SDKROOT"
        ;;
    Linux)
        command -v pkg-config >/dev/null 2>&1 || fail "GUI requires pkg-config, GTK3 and WebKitGTK development packages."
        pkg-config --exists gtk+-3.0 || fail "GUI requires GTK3 development packages (gtk+-3.0)."
        if pkg-config --exists webkit2gtk-4.1; then
            tags=$tags,webkit2_41
        elif ! pkg-config --exists webkit2gtk-4.0; then
            fail "GUI requires WebKitGTK development packages (webkit2gtk-4.1 or webkit2gtk-4.0)."
        fi
        ;;
    *) fail "Use start-gui.ps1 on Windows. This script supports macOS and Linux." ;;
esac
cd "$repo_dir"
mkdir -p bin
CGO_ENABLED=1 go build -tags "$tags" -o bin/conduit-gui ./cmd/conduit-gui
exec "$repo_dir/bin/conduit-gui" "$@"
