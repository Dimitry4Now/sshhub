#!/usr/bin/env bash
# One-time server preparation, run as root on the server (deploy.sh --setup
# does this over SSH). Safe to run again.
#   PORT  the port to open in the firewall (default 23234)
set -euo pipefail
PORT="${PORT:-23234}"

# A system user with no shell and no home: the service runs as this.
if ! id sshhub >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin sshhub
  echo "created user sshhub"
fi

install -m 0644 /tmp/sshhub.service /etc/systemd/system/sshhub.service
systemctl daemon-reload
systemctl enable sshhub >/dev/null
echo "installed and enabled sshhub.service"

# Oracle Cloud's Ubuntu images ship iptables rules that reject everything but
# SSH; insert an accept rule before the first REJECT and persist it. Other
# setups may use ufw instead.
if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
  ufw allow "$PORT/tcp"
elif ! iptables -C INPUT -p tcp -m state --state NEW --dport "$PORT" -j ACCEPT 2>/dev/null; then
  pos=$(iptables -L INPUT --line-numbers -n | awk '$2 == "REJECT" {print $1; exit}')
  iptables -I INPUT "${pos:-1}" -p tcp -m state --state NEW --dport "$PORT" -j ACCEPT
  if command -v netfilter-persistent >/dev/null; then
    netfilter-persistent save
  fi
  echo "opened tcp/$PORT in iptables"
fi
