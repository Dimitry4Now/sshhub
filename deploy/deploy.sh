#!/usr/bin/env bash
# Deploys and looks after sshhub on a server.
#
#   deploy/deploy.sh                    build the current checkout, prepare the server
#                                       (user, units, firewall), install, restart, check
#   deploy/deploy.sh --rollback         go back to the previously deployed binary
#   deploy/deploy.sh --pull-backup      take a backup now and copy it to ./backups
#   deploy/deploy.sh --restore FILE     replace the server's database with a snapshot
#   deploy/deploy.sh --add-admin [KEY]  allow a public key to run admin commands
#                                       (default ~/.ssh/id_ed25519.pub)
#
#   HOST  ssh destination for administration (default ubuntu@dibe-ssh-hub.duckdns.org)
#   PORT  the hub's port (default 23234; must match sshhub.service)
set -euo pipefail

HOST="${HOST:-ubuntu@dibe-ssh-hub.duckdns.org}"
PORT="${PORT:-23234}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NAME="${HOST#*@}"
STATE=/var/lib/sshhub

remote() { ssh -o BatchMode=yes "$HOST" "$@"; }
die() { echo "$*" >&2; exit 1; }

verify() {
  echo "› waiting for the service"
  for _ in $(seq 20); do
    remote systemctl is-active --quiet sshhub && break
    sleep 0.5
  done
  if ! remote systemctl is-active --quiet sshhub; then
    remote sudo journalctl -u sshhub -n 30 --no-pager
    die "sshhub is not running"
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
  die "the service runs but $NAME:$PORT doesn't answer from here; check the cloud firewall"
}

deploy() {
  case "$(remote uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64) arch=arm64 ;;
  *) die "unsupported server architecture" ;;
  esac

  if [[ -n "$(git -C "$ROOT" status --porcelain)" ]]; then
    echo "warning: deploying uncommitted changes (the version will say -dirty)" >&2
  fi

  echo "› building for linux/$arch"
  out="$(mktemp -d)"
  trap 'rm -rf "$out"' EXIT
  (cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$out/sshhub" .)

  echo "› preparing the server"
  remote 'rm -rf /tmp/sshhub-units && mkdir /tmp/sshhub-units'
  scp -q "$ROOT"/deploy/*.service "$ROOT"/deploy/*.timer "$HOST:/tmp/sshhub-units/"
  remote "sudo PORT=$PORT bash -s" < "$ROOT/deploy/setup.sh"

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
}

case "${1:-}" in
"")
  deploy
  ;;
--rollback)
  remote sudo test -f /usr/local/bin/sshhub.prev || die "no previous binary to roll back to"
  remote sudo cp -p /usr/local/bin/sshhub.prev /usr/local/bin/sshhub
  remote sudo systemctl restart sshhub
  verify
  ;;
--pull-backup)
  echo "› backing up on the server"
  remote sudo systemctl start sshhub-backup
  latest="$(remote "sudo sh -c 'ls -1 $STATE/backups/hub-*.db | tail -n 1'")"
  [[ -n "$latest" ]] || die "no backups on the server"
  mkdir -p "$ROOT/backups"
  chmod 700 "$ROOT/backups"
  # The files belong to the service user, so read them through sudo.
  remote "sudo cat '$latest'" > "$ROOT/backups/$(basename "$latest")"
  remote "sudo cat $STATE/backups/host_ed25519" > "$ROOT/backups/host_ed25519"
  chmod 600 "$ROOT/backups/"*
  echo "› saved backups/$(basename "$latest") and backups/host_ed25519"
  ;;
--restore)
  file="${2:-}"
  [[ -f "$file" ]] || die "usage: $0 --restore FILE"
  scp -q "$file" "$HOST:/tmp/sshhub-restore.db"
  # The stopped hub has checkpointed its WAL, so hub.db is complete; keep it
  # in case the restore was a mistake.
  remote "set -e
    sudo systemctl stop sshhub
    if sudo test -f $STATE/hub.db; then sudo cp -p $STATE/hub.db $STATE/hub.db.before-restore; fi
    sudo rm -f $STATE/hub.db-wal $STATE/hub.db-shm
    sudo install -o sshhub -g sshhub -m 0600 /tmp/sshhub-restore.db $STATE/hub.db
    rm -f /tmp/sshhub-restore.db
    sudo systemctl start sshhub"
  echo "› restored; the previous database is $STATE/hub.db.before-restore"
  verify
  ;;
--add-admin)
  key_file="${2:-$HOME/.ssh/id_ed25519.pub}"
  [[ -f "$key_file" ]] || die "no such key file: $key_file"
  ssh-keygen -lf "$key_file" >/dev/null || die "$key_file is not a public key"
  # Sent on stdin, so the key never goes through shell quoting.
  remote 'k="$(cat)"; sudo grep -qxF "$k" /etc/sshhub/admin_keys || echo "$k" | sudo tee -a /etc/sshhub/admin_keys >/dev/null' < "$key_file"
  echo "› admin: $(ssh-keygen -lf "$key_file")"
  ;;
*)
  die "usage: $0 [--rollback | --pull-backup | --restore FILE | --add-admin [KEY]]"
  ;;
esac
