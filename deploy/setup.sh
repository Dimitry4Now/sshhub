#!/usr/bin/env bash
# Server preparation, run as root on the server by deploy.sh before every
# deploy, with the unit files and sshhub-install in /tmp/sshhub-units. Safe to
# run again.
#   PORT  the port to open in the firewall (default 23234)
set -euo pipefail
PORT="${PORT:-23234}"

# A system user with no shell and no home: the service runs as this.
if ! id sshhub >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin sshhub
  echo "created user sshhub"
fi

# Admin keys live outside the service's reach: root owns them, the hub only
# reads them (see deploy.sh --add-admin).
install -d -m 0755 /etc/sshhub
[ -f /etc/sshhub/admin_keys ] || install -m 0644 /dev/null /etc/sshhub/admin_keys

install -m 0644 /tmp/sshhub-units/*.service /tmp/sshhub-units/*.timer /etc/systemd/system/
install -m 0755 /tmp/sshhub-units/sshhub-install /usr/local/sbin/sshhub-install
rm -rf /tmp/sshhub-units
systemctl daemon-reload
systemctl enable --quiet sshhub
systemctl enable --quiet --now sshhub-backup.timer

# CI deploys as "deploy", a user that can do one thing: run sshhub-install
# through sudo. Its authorized_keys belongs to root, so even a deploy session
# can't add keys (ci-key.sh writes it).
if ! id deploy >/dev/null 2>&1; then
  useradd --create-home --shell /bin/sh deploy
  echo "created user deploy"
fi
install -d -o root -g root -m 0755 /home/deploy/.ssh
[ -f /home/deploy/.ssh/authorized_keys ] || install -o root -g root -m 0644 /dev/null /home/deploy/.ssh/authorized_keys
sudoers="$(mktemp)"
echo 'deploy ALL=(root) NOPASSWD: /usr/local/sbin/sshhub-install ""' > "$sudoers"
visudo -cqf "$sudoers"
install -m 0440 "$sudoers" /etc/sudoers.d/sshhub-deploy
rm -f "$sudoers"

# Oracle Cloud's Ubuntu images ship iptables rules that reject everything but
# SSH; insert an accept rule before the first REJECT and persist it. Other
# setups may use ufw instead.
if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
  ufw allow "$PORT/tcp" >/dev/null
elif ! iptables -C INPUT -p tcp -m state --state NEW --dport "$PORT" -j ACCEPT 2>/dev/null; then
  pos=$(iptables -L INPUT --line-numbers -n | awk '$2 == "REJECT" {print $1; exit}')
  iptables -I INPUT "${pos:-1}" -p tcp -m state --state NEW --dport "$PORT" -j ACCEPT
  if command -v netfilter-persistent >/dev/null; then
    netfilter-persistent save
  fi
  echo "opened tcp/$PORT in iptables"
fi
