#!/usr/bin/env bash
set -euo pipefail

STATE_DIR="$HOME/.griffin-host"
PID_FILE="$STATE_DIR/host.pid"
LOG_FILE="$STATE_DIR/host.log"

if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "griffin-host is RUNNING (PID: $(cat "$PID_FILE"))."
    if [ -f "$LOG_FILE" ]; then
        echo "--- Recent Logs (last 5 lines) ---"
        tail -n 5 "$LOG_FILE"
    fi
else
    echo "griffin-host is STOPPED."
fi
