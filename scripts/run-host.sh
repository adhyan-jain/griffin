#!/usr/bin/env bash
set -euo pipefail

# Run Griffin host agent on this laptop.
export HOST_CAPABILITIES="${HOST_CAPABILITIES:-agent,messaging,web,desktop}"
export GRIFFIN_API_URL="${GRIFFIN_API_URL:?set GRIFFIN_API_URL to your API base URL, e.g. https://<api-id>.execute-api.<region>.amazonaws.com}"
export GRIFFIN_HOST_TOKEN_FILE="${GRIFFIN_HOST_TOKEN_FILE:-$HOME/.griffin-host/token}"

cd "$(dirname "$0")/.."
if [ -f "./build/griffin-host" ]; then
    exec ./build/griffin-host "$@"
else
    exec go run ./cmd/griffin-host "$@"
fi
