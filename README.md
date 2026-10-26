# sshhub

A hangout spot you reach over SSH: trivia, snake, a chat lobby with an online list, a meme gallery and a leaderboard, with a live clock in the header. No shell access — every connection lands in the TUI.

![The hub's main menu](docs/screenshots/menu.png)

## Features

**Chat lobby** — live chat with everyone connected, plus an online list showing who's chatting (●) and who's elsewhere in the hub (○).

![Chat lobby with the online list](docs/screenshots/chat.png)

<table>
  <tr>
    <td width="50%"><b>Trivia</b> — 10 random questions from a bank of 100+, 15 seconds each, faster answers score more.<br><br><img src="docs/screenshots/trivia.png" alt="Trivia question after answering"></td>
    <td width="50%"><b>Snake</b> — the classic, over SSH. It speeds up with every apple.<br><br><img src="docs/screenshots/snake.png" alt="Snake game in progress"></td>
  </tr>
  <tr>
    <td width="50%"><b>Meme gallery</b> — ASCII art tech memes, browse with ←/→.<br><br><img src="docs/screenshots/memes.png" alt="Doge meme in the gallery"></td>
    <td width="50%"><b>Leaderboard</b> — best score per player, one tab per game.<br><br><img src="docs/screenshots/leaderboard.png" alt="Trivia leaderboard"></td>
  </tr>
  <tr>
    <td width="50%"><b>Settings</b> — pick a theme, change your nickname, or wipe your scores (asks first).<br><br><img src="docs/screenshots/settings.png" alt="Settings screen previewing the Papaya theme"></td>
    <td width="50%"><b>Themes</b> — six color schemes, saved with your profile:<br><br>
      <b>Racing Green</b> (default) · <b>Rosso Corsa</b> · <b>Papaya</b> · <b>Silver</b> · <b>Midnight</b> · <b>Racing Blue</b><br><br>
      <sub>Inspired by the historic national racing colors and motorsport color traditions.</sub></td>
  </tr>
</table>

## Run

```sh
go build -o sshhub .
./sshhub                       # listens on :23234, creates hub.db and .ssh/id_ed25519
ssh -p 23234 localhost         # connect
```

Flags: `-addr`, `-db`, `-hostkey`, `-dev` (allow several sessions per person, handy for testing locally), `-max-conns` and `-max-conns-per-ip` (connections open at once, 200 and 10 by default), `-version`.

Everything that should outlive a restart lives in the SQLite database: profiles, scores, themes and the chat lobby's history (the last 100 messages are shown when you join). Stopping the server (Ctrl+C or SIGTERM) tells everyone connected that it's restarting and gives them a few seconds before closing.

The repo pins a newer Go toolchain in `go.mod`; with Go 1.21+ installed it is downloaded automatically.

## Identity

- Connecting with an SSH key: the key fingerprint is your account. You pick a nickname on first login and scores are saved.
- Connecting without a key (`ssh -o PubkeyAuthentication=no you@host`): guest session, named after your SSH username, scores not saved.
- Settings: key users can change their nickname (the leaderboard follows, since scores belong to the key) and clear all their scores. Theme choice is saved per key; guests keep it for the session.
- One session per person: logging in again (same key, or for guests same address and username) closes the older session. Run with `-dev` to turn this off.

## Deploy

`deploy/` runs the hub as a hardened systemd service on a Linux server (amd64 or arm64), with the database and host key in `/var/lib/sshhub`.

```sh
deploy/deploy.sh --setup     # first time: service user, systemd unit, firewall rule, then deploy
deploy/deploy.sh             # build the current checkout, install it, restart, check it answers
deploy/deploy.sh --rollback  # back to the previous binary
```

`HOST` picks the server (an SSH destination with sudo) and `PORT` the hub's port. On a cloud VM, the provider's firewall (for Oracle Cloud, the subnet's security list) must allow the port too. Logs: `journalctl -u sshhub -f`.

## CI

GitHub Actions checks formatting and runs `go vet`, the tests with the race detector, staticcheck and govulncheck, and cross-compiles for the server. To get a Telegram message when a run fails, create a bot with @BotFather and add `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` as repository secrets.

## Adding content

- Trivia: edit `assets/questions.json` (`answer` is the index into `choices`; choices are shuffled at runtime).
- Memes: drop `.txt` files of classic ASCII art into `assets/memes/`. A leading `NN-` sets the order. The tests keep them to plain ASCII and at most 70 columns by 20 lines.

Assets are embedded, so rebuild after changes.

## Screenshots

`scripts/screenshots/screenshots.sh` regenerates the images in `docs/screenshots/`. It runs a demo server with seeded players and chat bots, and records a scripted SSH session with [VHS](https://github.com/charmbracelet/vhs) in headless Chrome, so nothing opens on your desktop. Needs `vhs`, `ttyd`, `ffmpeg` and Chrome/Chromium.
