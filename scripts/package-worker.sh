#!/usr/bin/env bash
# Builds the EC2 worker bundle: linux/arm64 griffin-server + infra/ files + installer.
# Usage: scripts/package-worker.sh [output.tar.gz]   (default: build/griffin-worker.tar.gz)
# Runs fully offline; it does not touch AWS. Upload/transfer is a separate, explicit step.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
OUT="${1:-build/griffin-worker.tar.gz}"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$STAGE/griffin-server" ./cmd/griffin-server
mkdir -p "$STAGE/infra"
cp -r infra/bin infra/systemd infra/sudoers.d "$STAGE/infra/"
cp infra/install-worker.sh "$STAGE/install-worker.sh"

file "$STAGE/griffin-server" | grep -q "ARM aarch64" || { echo "griffin-server is not arm64" >&2; exit 1; }
mkdir -p "$(dirname "$OUT")"
tar -C "$STAGE" -czf "$OUT" .
tar -tzf "$OUT"
sha256sum "$OUT"
