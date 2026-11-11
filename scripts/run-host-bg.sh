#!/usr/bin/env bash
set -euo pipefail

STATE_DIR="$HOME/.griffin-host"
PID_FILE="$STATE_DIR/host.pid"
LOG_FILE="$STATE_DIR/host.log"
mkdir -p "$STATE_DIR"

if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "griffin-host is already running in background (PID: $(cat "$PID_FILE"))."
    echo "Logs: $LOG_FILE"
    exit 0
fi

cd "$(dirname "$0")/.."
nohup ./scripts/run-host.sh > "$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"

sleep 1
if kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "Started griffin-host in background (PID: $(cat "$PID_FILE"))."
    echo "You can close this terminal anytime. Griffin will stay online."
    echo "Logs: $LOG_FILE"
    echo "To stop: make stop-host"
else
    echo "griffin-host failed to start in background. Check $LOG_FILE"
    exit 1
fi
