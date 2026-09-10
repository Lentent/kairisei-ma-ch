#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
TARGET=${1:-all}
case "$TARGET" in all|windows-amd64|linux-amd64|linux-arm64) ;; *) echo 'Usage: sh tools/sh/Build-Server.sh [all|windows-amd64|linux-amd64|linux-arm64] [--skip-tests|--dry-run]' >&2; exit 2 ;; esac
MODE=${2:-check}
case "$MODE" in check|--skip-tests|--dry-run) ;; *) echo 'Unknown build option.' >&2; exit 2 ;; esac
[ "$#" -le 2 ] || exit 2
if [ "$MODE" = --dry-run ]; then printf 'Build %s from %s/server into %s/_local/bin\n' "$TARGET" "$ROOT" "$ROOT"; exit 0; fi
command -v go >/dev/null
cd "$ROOT/server"
if [ "$MODE" = check ]; then
    (unset GOOS GOARCH; export CGO_ENABLED=0; go test ./...; go vet ./...)
fi
mkdir -p "$ROOT/_local/bin"
TARGETS=$TARGET
if [ "$TARGET" = all ]; then TARGETS='windows-amd64 linux-amd64 linux-arm64'; fi
for target in $TARGETS; do
    os=${target%-*}
    arch=${target#*-}
    name=kairi-server-$target
    if [ "$os" = windows ]; then name=kairi-server.exe; fi
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -buildvcs=false -trimpath '-ldflags=-s -w' -o "$ROOT/_local/bin/$name" ./cmd/kairi-server
    printf 'Built %s\n' "$ROOT/_local/bin/$name"
done
