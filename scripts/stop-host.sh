#!/usr/bin/env bash
set -euo pipefail

STATE_DIR="$HOME/.griffin-host"
PID_FILE="$STATE_DIR/host.pid"

stopped=0
if [ -f "$PID_FILE" ]; then
    PID="$(cat "$PID_FILE")"
    if kill -0 "$PID" 2>/dev/null; then
        echo "Stopping griffin-host (PID $PID)..."
        kill "$PID" 2>/dev/null || true
        for _ in {1..10}; do
            if ! kill -0 "$PID" 2>/dev/null; then
                break
            fi
            sleep 0.5
        done
        if kill -0 "$PID" 2>/dev/null; then
            kill -9 "$PID" 2>/dev/null || true
        fi
        stopped=1
    fi
    rm -f "$PID_FILE"
fi

# Also clean up any lingering griffin-host processes
if pkill -f "cmd/griffin-host" 2>/dev/null; then
    stopped=1
fi

if [ $stopped -eq 1 ]; then
    echo "griffin-host stopped."
else
    echo "griffin-host is not currently running."
fi
