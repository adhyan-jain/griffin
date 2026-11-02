#!/usr/bin/env bash
# Idempotent installer for the Griffin worker on the EC2 runner (Ubuntu 24.04 arm64).
# Run as root from the unpacked worker bundle (see scripts/package-worker.sh), e.g. via SSM:
#   sudo bash install-worker.sh
# It does NOT start the service: authenticate agy first (docs/DEPLOYMENT_RUNBOOK.md).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
[ "$(uname -m)" = "aarch64" ] || { echo "expected aarch64, got $(uname -m)" >&2; exit 1; }
id griffin >/dev/null 2>&1 || { echo "user 'griffin' missing (run infra/user-data.sh first)" >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y dbus-user-session unzip curl

if ! command -v aws >/dev/null 2>&1; then
  tmp="$(mktemp -d)"
  curl -fsSL https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip -o "$tmp/awscliv2.zip"
  unzip -q "$tmp/awscliv2.zip" -d "$tmp"
  "$tmp/aws/install"
  rm -rf "$tmp"
fi

install -d -o griffin -g griffin -m 755 /opt/griffin
install -d -o root -g griffin -m 750 /etc/griffin
install -o griffin -g griffin -m 755 "$HERE/griffin-server" /opt/griffin/griffin-server
install -o root -g root -m 750 "$HERE/infra/bin/griffin-fetch-env.sh" /usr/local/bin/griffin-fetch-env.sh
install -o root -g root -m 644 "$HERE/infra/systemd/griffin-worker.service" /etc/systemd/system/griffin-worker.service

# sudoers: validate before installing so a typo can never lock out sudo.
install -m 440 "$HERE/infra/sudoers.d/griffin-shutdown" /etc/sudoers.d/griffin-shutdown.new
visudo -cf /etc/sudoers.d/griffin-shutdown.new
mv -f /etc/sudoers.d/griffin-shutdown.new /etc/sudoers.d/griffin-shutdown

loginctl enable-linger griffin
systemctl daemon-reload
echo "installed. Next: authenticate agy as 'griffin', verify, then: systemctl enable --now griffin-worker"
