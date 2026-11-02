#!/usr/bin/env bash
# Builds the API Lambda bundle: a linux/arm64 `bootstrap` binary (provided.al2023) in a zip.
# Migrations are embedded in the binary and are NOT run by the server, so nothing else is shipped.
# Usage: scripts/package-lambda.sh [output.zip]   (default: build/griffin-api.zip)
# Runs fully offline; it does not touch AWS.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
OUT="${1:-build/griffin-api.zip}"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$STAGE/bootstrap" ./cmd/griffin-server
mkdir -p "$(dirname "$OUT")"
OUT_ABS="$(cd "$(dirname "$OUT")" && pwd)/$(basename "$OUT")"
rm -f "$OUT_ABS"
(cd "$STAGE" && zip -q -X "$OUT_ABS" bootstrap)

file "$STAGE/bootstrap" | grep -q "ARM aarch64" || { echo "bootstrap is not arm64" >&2; exit 1; }
unzip -l "$OUT_ABS"
sha256sum "$OUT_ABS"
