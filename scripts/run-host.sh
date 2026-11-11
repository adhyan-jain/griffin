#!/usr/bin/env bash
set -euo pipefail

# Run Griffin host agent on this laptop.
export HOST_CAPABILITIES="${HOST_CAPABILITIES:-agent,messaging,web}"
export GRIFFIN_API_URL="${GRIFFIN_API_URL:-https://fq322jvjt4.execute-api.ap-south-1.amazonaws.com}"
export GRIFFIN_HOST_TOKEN_FILE="${GRIFFIN_HOST_TOKEN_FILE:-$HOME/.griffin-host/token}"

cd "$(dirname "$0")/.."
exec go run ./cmd/griffin-host "$@"
