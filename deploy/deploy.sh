#!/usr/bin/env bash
# Builds sshhub for the server, installs it, restarts the service and checks
# that it answers from the outside.
#
#   deploy/deploy.sh             build and deploy the current checkout
#   deploy/deploy.sh --setup     prepare a new server first (user, unit, firewall)
#   deploy/deploy.sh --rollback  go back to the previously deployed binary
#
#   HOST  ssh destination for administration (default ubuntu@dibe-ssh-hub.duckdns.org)
#   PORT  the hub's port (default 23234; must match sshhub.service)
set -euo pipefail

HOST="${HOST:-ubuntu@dibe-ssh-hub.duckdns.org}"
PORT="${PORT:-23234}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NAME="${HOST#*@}"

remote() { ssh -o BatchMode=yes "$HOST" "$@"; }

verify() {
  echo "› waiting for the service"
  for _ in $(seq 20); do
    remote systemctl is-active --quiet sshhub && break
    sleep 0.5
  done
  if ! remote systemctl is-active --quiet sshhub; then
    remote sudo journalctl -u sshhub -n 30 --no-pager
    echo "sshhub is not running" >&2
    return 1
  fi
  # An SSH handshake from here proves the port is open end to end
  # (service, host firewall and cloud firewall).
  for _ in $(seq 20); do
    if ssh-keyscan -T 3 -p "$PORT" "$NAME" 2>/dev/null | grep -q ssh-ed25519; then
      echo "› reachable: ssh -p $PORT $NAME"
      remote 'sudo journalctl -u sshhub -n 1 --no-pager -g "starting ssh hub" -o cat'
      return 0
    fi
    sleep 0.5
  done
  echo "the service runs but $NAME:$PORT doesn't answer from here; check the cloud firewall" >&2
  return 1
}

case "${1:-}" in
--rollback)
  remote sudo test -f /usr/local/bin/sshhub.prev || { echo "no previous binary to roll back to" >&2; exit 1; }
  remote sudo cp -p /usr/local/bin/sshhub.prev /usr/local/bin/sshhub
  remote sudo systemctl restart sshhub
  verify
  exit
  ;;
--setup)
  scp -q "$ROOT/deploy/sshhub.service" "$HOST:/tmp/sshhub.service"
  remote "sudo PORT=$PORT bash -s" < "$ROOT/deploy/setup.sh"
  ;;
"") ;;
*)
  echo "usage: $0 [--setup|--rollback]" >&2
  exit 2
  ;;
esac

case "$(remote uname -m)" in
x86_64) arch=amd64 ;;
aarch64) arch=arm64 ;;
*) echo "unsupported server architecture" >&2; exit 1 ;;
esac

if [[ -n "$(git -C "$ROOT" status --porcelain)" ]]; then
  echo "warning: deploying uncommitted changes (the version will say -dirty)" >&2
fi

echo "› building for linux/$arch"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$out/sshhub" .)

echo "› installing"
scp -q "$out/sshhub" "$HOST:/tmp/sshhub.new"
# Keep the running binary for --rollback, swap in the new one atomically.
remote 'set -e
  if [ -f /usr/local/bin/sshhub ]; then sudo cp -p /usr/local/bin/sshhub /usr/local/bin/sshhub.prev; fi
  sudo install -m 0755 /tmp/sshhub.new /usr/local/bin/sshhub.tmp
  sudo mv /usr/local/bin/sshhub.tmp /usr/local/bin/sshhub
  rm -f /tmp/sshhub.new
  sudo systemctl restart sshhub'
verify
