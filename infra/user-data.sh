#!/usr/bin/env bash
# Griffin EC2 Cloud-Init Script
# Target: Ubuntu 24.04 ARM64 (Graviton2) in ap-south-1
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# 1. Update OS and install prerequisites
apt-get update -y
apt-get upgrade -y
apt-get install -y curl wget git jq unzip ca-certificates dbus libsecret-1-0 libsecret-tools gnome-keyring

# 2. Ensure Amazon SSM Agent is enabled and active
# On Ubuntu 24.04 AWS AMIs the agent ships as a snap (there is no apt package); do not install it here.
if snap list amazon-ssm-agent >/dev/null 2>&1; then
    systemctl enable snap.amazon-ssm-agent.amazon-ssm-agent.service
    systemctl restart snap.amazon-ssm-agent.amazon-ssm-agent.service
else
    echo "WARNING: amazon-ssm-agent snap not found; Session Manager will not work" >&2
fi

# 3. Create dedicated unprivileged 'griffin' user
if ! id -u griffin >/dev/null 2>&1; then
    useradd -m -s /bin/bash -u 1001 griffin
fi

# 4. Enable systemd user lingering for griffin (persists headless DBus and user services)
loginctl enable-linger griffin

# 5. Configure 2 GiB swapfile for OOM protection
if [ ! -f /swapfile ]; then
    fallocate -l 2G /swapfile
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile
    echo '/swapfile none swap sw 0 0' >> /etc/fstab
    sysctl vm.swappiness=10
    echo 'vm.swappiness=10' >> /etc/sysctl.d/99-griffin.conf
fi

# 6. Create scratch workspace directory with proper permissions
mkdir -p /var/griffin/workspaces
chown -R griffin:griffin /var/griffin

# 7. Install Google Antigravity CLI for griffin user
su - griffin -c "curl -fsSL https://antigravity.google/cli/install.sh | bash"

# 8. Configure environment variables in /home/griffin/.bashrc
if ! grep -q "PATH=.*\.local/bin" /home/griffin/.bashrc; then
    echo 'export PATH="$HOME/.local/bin:$PATH"' >> /home/griffin/.bashrc
    echo 'export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/1001/bus"' >> /home/griffin/.bashrc
fi

# 9. Verify CLI installation
su - griffin -c "export PATH=\"\$HOME/.local/bin:\$PATH\" && agy --version" || true

# 10. /etc/griffin (env file written by griffin-fetch-env.sh). The systemd unit is installed ONLY by
# infra/install-worker.sh from infra/systemd/griffin-worker.service (single source of truth).
mkdir -p /etc/griffin
chmod 750 /etc/griffin
chown root:griffin /etc/griffin

echo "Griffin EC2 initialization completed successfully at $(date -u)"

