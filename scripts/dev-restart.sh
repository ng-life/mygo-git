#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$project_dir"
mkdir -p bin
# Build first: a failed build must leave the running app untouched.
build_file="$(mktemp "$project_dir/bin/.mygo-git-build.XXXXXX")"
trap 'rm -f "$build_file"' EXIT
CGO_ENABLED=0 go build -o "$build_file" .
python3 scripts/restart.py "$project_dir" "$build_file"
