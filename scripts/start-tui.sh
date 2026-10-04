#!/bin/sh
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
command -v go >/dev/null 2>&1 || { echo "Go is required. Install Go and add it to PATH." >&2; exit 1; }
cd "$repo_dir"
mkdir -p bin
go build -o bin/conduit ./cmd/conduit
exec "$repo_dir/bin/conduit" "$@"
