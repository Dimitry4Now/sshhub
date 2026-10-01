#!/usr/bin/env bash
# Regenerates the README screenshots in docs/screenshots/.
#
# It starts a demo server on a throwaway database, connects a few bot users
# who chat in the lobby, then drives a real SSH session with VHS
# (https://github.com/charmbracelet/vhs), which renders in headless Chrome:
# no window opens, and your terminal's theme or transparency doesn't matter.
#
# Needs: go, ssh, ssh-keygen, script, vhs, ttyd, ffmpeg, Chrome/Chromium.
#   go install github.com/charmbracelet/vhs@latest
#   sudo apt install ttyd ffmpeg
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/docs/screenshots"
PORT="${PORT:-23299}"

for cmd in go ssh ssh-keygen script vhs ttyd ffmpeg; do
  command -v "$cmd" >/dev/null || { echo "missing dependency: $cmd" >&2; exit 1; }
done

WORK="$(mktemp -d)"
SERVER_PID=""
BOT_PIDS=()
cleanup() {
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
  for pid in "${BOT_PIDS[@]}"; do kill -- "-$pid" 2>/dev/null || true; done
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "› building"
(cd "$ROOT" && go build -o "$WORK/sshhub" .)

echo "› creating demo users"
mkdir -p "$WORK/keys" "$WORK/img"
for nick in dimitar ada linus grace ken; do
  ssh-keygen -q -t ed25519 -N '' -C "$nick" -f "$WORK/keys/$nick"
  echo "$nick $(ssh-keygen -lf "$WORK/keys/$nick.pub" | awk '{print $2}')"
done > "$WORK/fingerprints.txt"
(cd "$ROOT" && go run ./scripts/screenshots/seed "$WORK/demo.db" "$WORK/fingerprints.txt")

echo "› starting demo server on :$PORT"
"$WORK/sshhub" -addr "127.0.0.1:$PORT" -db "$WORK/demo.db" -hostkey "$WORK/hostkey" >"$WORK/server.log" 2>&1 &
SERVER_PID=$!
for _ in $(seq 50); do
  (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && break
  sleep 0.1
done

SSH_OPTS="-o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -p $PORT"

# bot <nick> <menu key> [delay:message ...] keeps a user connected for a while,
# optionally opening a menu item and posting chat messages.
bot() {
  local nick=$1 key=$2; shift 2
  setsid bash -c '
    nick=$1 key=$2 work=$3 opts=$4; shift 4
    { sleep 2; printf "%s" "$key"
      for m in "$@"; do sleep "${m%%:*}"; printf "%s\r" "${m#*:}"; done
      sleep 120
    } | script -qc "stty cols 120 rows 40; ssh -tt $opts -i $work/keys/$nick localhost" /dev/null >/dev/null 2>&1
  ' _ "$nick" "$key" "$WORK" "$SSH_OPTS" "$@" &
  BOT_PIDS+=($!)
}

echo "› connecting bots"
bot ada   3 "1:morning! who's up for trivia?" "6:anyone beat 1320 yet? grace is untouchable"
bot linus 3 "3:already did two rounds, the SQL ones got me" "6:also: never push --force on a friday"
bot grace 3 "5:HAVING vs WHERE, classic"
bot ken   "" # stays in the menu, so the online list shows someone elsewhere
sleep 15

echo "› recording with vhs"
cat > "$WORK/hub.tape" <<TAPE
Output demo.gif
Set Shell bash
Set FontSize 20
Set Width 1920
Set Height 1080
Set Padding 24
Set TypingSpeed 40ms

Hide
Type "ssh $SSH_OPTS -i keys/dimitar localhost"
Enter
Sleep 3s
Show
Sleep 500ms
Screenshot img/menu.png

Type "1"
Sleep 1s
Type "a"
Sleep 700ms
Screenshot img/trivia.png
Escape
Sleep 700ms

Type "2"
Sleep 900ms
Screenshot img/snake.png
Escape
Sleep 700ms

Type "3"
Sleep 1s
Type "hey all, welcome to the hub!"
Enter
Sleep 1s
Screenshot img/chat.png
Escape
Sleep 700ms

Type "4"
Sleep 500ms
Right
Right
Sleep 700ms
Screenshot img/memes.png
Escape
Sleep 700ms

Type "5"
Sleep 1s
Screenshot img/leaderboard.png
TAPE
(cd "$WORK" && vhs hub.tape)

mkdir -p "$OUT"
cp "$WORK"/img/*.png "$OUT/"
echo "✔ screenshots written to ${OUT#"$ROOT/"}/"
ls -1 "$OUT"
