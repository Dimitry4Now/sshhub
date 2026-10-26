#!/usr/bin/env bash
# Creates (or replaces) the key GitHub Actions deploys with.
#
# A fresh key pair is made here; the public half goes on the server, limited
# to running sshhub-install, and the private half becomes a secret of the
# repository's "production" environment, which only master may use. The
# private key is then deleted locally. Run again to rotate the key.
#
# Needs gh (logged in) and the same ssh access as deploy.sh.
#   HOST  ssh destination for administration (default ubuntu@dibe-ssh-hub.duckdns.org)
#   ARCH  GOARCH of the server (default arm64)
set -euo pipefail

HOST="${HOST:-ubuntu@dibe-ssh-hub.duckdns.org}"
ARCH="${ARCH:-arm64}"
NAME="${HOST#*@}"
REPO="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"

die() { echo "$*" >&2; exit 1; }

# The server's host key, as already trusted by this machine, so the CI checks
# it instead of trusting whatever answers.
known="$(ssh-keygen -F "$NAME" | grep -v '^#' || true)"
[[ -n "$known" ]] || die "$NAME is not in ~/.ssh/known_hosts; ssh to it once first"
known="$(printf '%s\n' "$known" | awk -v h="$NAME" '{ $1 = h; print }')"

echo "› production environment: deployable from master only"
gh api -X PUT "repos/$REPO/environments/production" --input - >/dev/null <<JSON
{"deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}
JSON
if ! gh api "repos/$REPO/environments/production/deployment-branch-policies" --jq '.branch_policies[].name' | grep -qx master; then
  gh api -X POST "repos/$REPO/environments/production/deployment-branch-policies" -f name=master -f type=branch >/dev/null
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
ssh-keygen -q -t ed25519 -N '' -C "sshhub-ci@$REPO" -f "$work/key"

echo "› installing the public key on the server"
line="restrict,command=\"sudo -n /usr/local/sbin/sshhub-install\" $(cat "$work/key.pub")"
printf '%s\n' "$line" | ssh -o BatchMode=yes "$HOST" 'sudo tee /home/deploy/.ssh/authorized_keys >/dev/null'

echo "› storing the private key and settings in GitHub"
gh secret set DEPLOY_SSH_KEY --env production --repo "$REPO" < "$work/key"
gh variable set DEPLOY_HOST --env production --repo "$REPO" --body "$NAME"
gh variable set DEPLOY_KNOWN_HOSTS --env production --repo "$REPO" --body "$known"
gh variable set DEPLOY_GOARCH --env production --repo "$REPO" --body "$ARCH"

echo "› done: $(ssh-keygen -lf "$work/key.pub")"
