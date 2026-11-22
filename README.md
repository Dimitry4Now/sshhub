# sshhub

A little online hangout that lives in your terminal. Play trivia and snake, chat with whoever's around, browse ASCII art memes and climb the leaderboard. Nothing to install: if you have a terminal, you're one command away.

```sh
ssh -p 23234 dibe-ssh-hub.duckdns.org
```

![The hub's main menu](docs/screenshots/menu.png)

## What's inside

**Chat lobby**: live chat with everyone connected, plus a list of who's online: chatting (●) or somewhere else in the hub (○). The last 100 messages are waiting for you when you join; messages are deleted after 30 days.

![Chat lobby with the online list](docs/screenshots/chat.png)

<table>
  <tr>
    <td width="50%"><b>Trivia</b>: 10 random questions from a bank of 100+, 15 seconds each, faster answers score more.<br><br><img src="docs/screenshots/trivia.png" alt="Trivia question after answering"></td>
    <td width="50%"><b>Snake</b>: the classic. It speeds up with every apple.<br><br><img src="docs/screenshots/snake.png" alt="Snake game in progress"></td>
  </tr>
  <tr>
    <td width="50%"><b>Meme gallery</b>: ASCII art tech memes, browse with ←/→.<br><br><img src="docs/screenshots/memes.png" alt="Doge meme in the gallery"></td>
    <td width="50%"><b>Leaderboard</b>: best score per player, one tab per game.<br><br><img src="docs/screenshots/leaderboard.png" alt="Trivia leaderboard"></td>
  </tr>
  <tr>
    <td width="50%"><b>Settings</b>: pick a theme, change your nickname, or wipe your scores (asks first).<br><br><img src="docs/screenshots/settings.png" alt="Settings screen previewing the Papaya theme"></td>
    <td width="50%"><b>Themes</b>: six color schemes, saved with your profile:<br><br>
      <b>Racing Green</b> (default) · <b>Rosso Corsa</b> · <b>Papaya</b> · <b>Silver</b> · <b>Midnight</b> · <b>Racing Blue</b><br><br>
      <sub>Inspired by the historic national racing colors and motorsport color traditions.</sub></td>
  </tr>
</table>

## How to join

SSH is the tool programmers use to log in to servers. Here it opens the hub instead, and the hub is all you can reach: there's no access to the server behind it.

1. Open a terminal:
   - **Windows 10 or 11**: PowerShell or Windows Terminal (SSH is built in).
   - **macOS**: the Terminal app.
   - **Linux**: any terminal.
   - **Phone**: an SSH app such as Termius.
2. Run `ssh -p 23234 dibe-ssh-hub.duckdns.org`.
3. The first time, SSH asks whether you trust the server. Type `yes`. To be thorough, check the fingerprint it shows is `SHA256:OmGMW7AavE9TTN+PPEzdXjk4KRo3ihYJXo8R52cBZ9g`.
4. Use the arrow keys or numbers to move around, Enter to pick, Esc to go back, `q` to leave.

**Your account is your SSH key.** If you have one (most developers do), the hub recognizes you every time: you pick a nickname once, and your scores and theme are saved. No passwords, no sign-up.

**No key? You're a guest.** You can do everything, but nothing is saved, and your name is whatever you put before the `@`:

```sh
ssh -p 23234 -o PubkeyAuthentication=no yourname@dibe-ssh-hub.duckdns.org
```

To get a key, run `ssh-keygen -t ed25519` once and press Enter at every question.

One session at a time: logging in again (same key, or as a guest from the same address with the same name) closes your older session. A session with no key presses for 15 minutes closes itself (it warns you a minute before). In Settings you can change your nickname (your scores follow you) or clear your scores.

---

## How it works

Everything is a single Go program. It speaks SSH itself, so it doesn't need the server's own SSH or a shell: each connection gets its own instance of the app, drawn with [Bubble Tea](https://github.com/charmbracelet/bubbletea), while the things everyone shares (who's online, the chat) live in one place in memory. Persistent data is in SQLite.

```mermaid
flowchart TB
    client["Your terminal<br/>ssh"] -->|"TCP :23234"| caps["Connection caps<br/>100 total, 10 per address"]
    caps --> handshake["SSH handshake<br/>key = account<br/>no key = guest"]
    handshake --> mw["Logging and<br/>rate limit"]
    mw -->|"ssh host who"| admin["Admin commands<br/>key allowlist + audit log"]
    mw -->|"interactive"| gate["Ban check"]
    gate --> window["Terminal size cap<br/>300 x 100"]
    window --> tui["The app<br/>one per session"]
    tui <--> hub["Hub<br/>presence, chat, kicks"]
    admin --> hub
    hub -->|"archive channel"| writer["Archive<br/>goroutine"]
    writer --> db[("SQLite<br/>profiles, scores,<br/>chat, audit log")]
    tui --> db
    admin --> db
    db -.->|"nightly"| backups[("Snapshots")]
```

The hub is guarded by a single mutex. Each chat member has a buffered channel; a message is handed to every channel and to the archive goroutine without ever waiting, so one slow connection or a slow disk can't hold up the chat. A reader that falls too far behind skips messages instead:

```mermaid
sequenceDiagram
    participant A as Alice
    participant H as Hub
    participant B as Bob
    participant W as Archive
    participant D as SQLite
    A->>H: Send("hi")
    H->>H: clean, rate limit
    H-->>A: own copy
    H-->>B: channel (skip if behind)
    H-->>W: archive channel
    W->>D: INSERT
```

Stopping the server (Ctrl+C, SIGTERM, `systemctl restart`) tells everyone the hub is restarting and gives them a few seconds to see it, then lets the archive finish before the database closes.

### Project layout

```
main.go                flags, backups, startup and shutdown
server.go              the server: middleware, the app per session, the hub
integration_test.go    the whole server tested over SSH
internal/app           the app: one Bubble Tea model per session, a screen per page
internal/hub           shared state: who's online, the chat, kicks and announcements
internal/store         SQLite: schema, migrations, queries
internal/admin         admin commands over SSH
internal/limit         connection caps and the terminal size cap
internal/backup        database snapshots and pruning
assets/                trivia questions and memes, embedded into the binary
deploy/                systemd units, server setup, deploy and CI key scripts
scripts/screenshots/   regenerates the screenshots in this README
```

### Run it yourself

```sh
go build -o sshhub .
./sshhub                 # listens on :23234, creates hub.db and .ssh/id_ed25519
ssh -p 23234 localhost
```

The repo pins its Go version in `go.mod`; with Go 1.21 or newer installed, the right toolchain is downloaded automatically.

| Flag | Default | |
|---|---|---|
| `-addr` | `:23234` | listen address |
| `-db` | `hub.db` | SQLite database |
| `-hostkey` | `.ssh/id_ed25519` | server key, created if missing |
| `-dev` | off | allow several sessions per person, for local testing |
| `-max-conns`, `-max-conns-per-ip` | 100, 10 | connections open at once |
| `-admin-keys` | none | authorized_keys file of admins (see [Administration](#administration)) |
| `-chat-days` | 30 | delete chat messages older than this; 0 keeps them |
| `-backup DIR`, `-keep N` | | snapshot the database and host key into DIR, keep N, exit |
| `-version` | | print the version and exit |

### Tests

```sh
go test -race ./...                        # unit tests: each package on its own
go test -race -tags integration .          # integration tests: the whole server over SSH
```

The integration tests start the real server, with all its middleware, on a local port and use an SSH client the way visitors and admins do: a guest's visit, a new key picking a nickname, chat between two people, admin commands, bans, the connection and session limits, and the restart notice.

To see coverage for both together:

```sh
pkgs=$(go list ./... | grep -v /scripts/ | paste -sd, -)
mkdir -p /tmp/cov/unit /tmp/cov/integration
go test -coverpkg="$pkgs" ./... -args -test.gocoverdir=/tmp/cov/unit
go test -tags integration -coverpkg="$pkgs" . -args -test.gocoverdir=/tmp/cov/integration
go tool covdata percent -i=/tmp/cov/unit,/tmp/cov/integration
```

### Security

It's a public server anyone can reach, so:

- **Nobody gets a shell.** Every connection lands in the app; admins get a fixed list of commands.
- **Resource caps.** At most 100 connections, 10 per address (an IPv6 /64 counts as one address, since one subscriber gets a whole /64), and a rate limit on new sessions. Sessions nobody types in close after 15 minutes, so idle visitors can't hold the slots. Terminal sizes are capped at 300 × 100 cells: drawing costs memory with every cell, and without the cap a single visitor claiming a giant window could run the server out of memory. With every connection at the cap, the server stays well inside its memory limit (measured).
- **Clean input.** Chat strips control characters, so nobody can send escape sequences to other people's terminals, and drops invisible formatting (bidi overrides, zero-width characters) and piles of accents ("Zalgo" text); nicknames are letters, digits, `_` and `-`. Each address gets a few "joined the chat" notices, then one per 30 seconds, so nobody can flood the chat by going in and out.
- **Bans.** Admins can ban a key, an address, or both at once for a session, for a while or for good; banned visitors are turned away before they reach the hub.
- **A sandboxed service.** It runs as its own user, can only write its data directory, and has no capabilities or access to other processes (`systemd-analyze security` rates it 1.4, "OK").
- **Accountable admins.** Admin keys live in a root-owned file outside the app's reach, and every command is written to an audit log before it runs, or doesn't run.
- **A narrow deploy key.** CI's key can only install a new binary (details below).

## Administration

Keys listed in the server's `/etc/sshhub/admin_keys` (authorized_keys format, owned by root) can run commands by connecting with one:

```sh
ssh -p 23234 dibe-ssh-hub.duckdns.org who                  # sessions: id, nick, where, for how long, address
ssh -p 23234 dibe-ssh-hub.duckdns.org kick 7               # or kick NICK
ssh -p 23234 dibe-ssh-hub.duckdns.org broadcast restarting in 5 minutes
ssh -p 23234 dibe-ssh-hub.duckdns.org stats                # version, uptime, sessions, memory
ssh -p 23234 dibe-ssh-hub.duckdns.org ban 7 7d             # ban session 7's key and address for a week
ssh -p 23234 dibe-ssh-hub.duckdns.org bans                 # bans in force, with their IDs
ssh -p 23234 dibe-ssh-hub.duckdns.org unban 3              # lift ban 3
ssh -p 23234 dibe-ssh-hub.duckdns.org audit                # the last admin commands
```

The file is read on every command, so adding or removing a key applies at once. Anyone else gets the same refusal whatever they type. `broadcast` shows a banner on every screen for 15 seconds and posts it in the chat.

`ban TARGET [FOR]` takes a session ID from `who` (bans that session's key and address), the nickname of a registered player (their key, and the address of any session they have open), a key fingerprint (`SHA256:...`) or an address (IPv6 addresses ban their /64). `FOR` is a length like `30m`, `12h` or `7d`, or `forever`; the default is 24 hours. It closes the banned sessions right away, and banning the same key or address again replaces the earlier ban. Admin keys can't be banned, and admin commands work from a banned address, so a ban never locks an admin out.

## Releasing and operations

### Workflow

```mermaid
flowchart TB
    branch["Feature branch"] -->|"pull request"| ci{"CI: lint, unit and<br/>integration tests,<br/>coverage, builds"}
    ci -->|"passes, merge"| develop["develop"]
    develop -->|"git tag v1.2.3"| tag["Release tag"]
    tag --> ci2{"CI again, and<br/>is the tag on develop?"}
    ci2 -->|"build, SSH as deploy"| install["sshhub-install<br/>swap binary, restart"]
    install -->|"listening"| live["Live"]
    install -->|"not listening"| rollback["Previous binary<br/>restored"]
```

- Work on a branch and open a pull request into `develop`. CI has to pass and the branch has to be up to date before it can merge.
- CI runs these jobs side by side:
  - **lint**: gofmt, tidy module files, `go vet`, staticcheck and govulncheck (pinned in `go.mod`), and ShellCheck on the deploy and screenshot scripts.
  - **unit tests** and **integration tests**, with the race detector.
  - **coverage**: both kinds of tests merged into a per-package table in the run's summary. It fails below 75%.
  - **build**: the server binary for linux/amd64 and linux/arm64.

  The `test` job, the one the ruleset requires, passes only when all of them do, and sends the Telegram message when one fails.
- To release, tag a commit on `develop`. The hub then shows the tag as its version:
  ```sh
  git switch develop && git pull
  git tag -a v0.2.0 -m "v0.2.0" && git push origin v0.2.0
  ```
- Nobody pushes to `develop` directly, force-pushes or deletes it, and release tags can't be moved or deleted (repository rulesets).

The deploy job pipes the binary over SSH to the server as the `deploy` user, whose key can do exactly one thing: run `sshhub-install`. That script accepts only an executable for the server's architecture, keeps the previous binary, restarts the hub and rolls back if it doesn't start listening. The key is a secret of the `production` environment, which only release tags can use; the server's host key is pinned, the actions the job runs are pinned to commits, and it builds without any cache. `deploy/ci-key.sh` creates or rotates the key.

To get a Telegram message when CI fails or a deploy finishes, create a bot with @BotFather and add `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` as repository secrets.

### Server

`deploy/deploy.sh` sets up and looks after a Linux server (amd64 or arm64) over SSH. The hub runs as the `sshhub` systemd service with its database and host key in `/var/lib/sshhub`.

```sh
deploy/deploy.sh                    # set up the server (users, units, firewall), build, install, restart, check
deploy/deploy.sh --rollback         # back to the previous binary
deploy/deploy.sh --pull-backup      # back up now and copy the snapshot and host key to ./backups
deploy/deploy.sh --restore FILE     # put a snapshot back (the replaced database is kept next to it)
deploy/deploy.sh --add-admin [KEY]  # let a public key run admin commands (default ~/.ssh/id_ed25519.pub)
```

CI only ships the binary; changes to the systemd units, firewall or users go out with `deploy/deploy.sh`. To roll back to an older release, check out its tag and run `deploy/deploy.sh`.

`HOST` picks the server (an SSH destination with sudo) and `PORT` the hub's port. On a cloud VM, the provider's firewall (for Oracle Cloud, the subnet's security list) has to allow the port too. Logs: `journalctl -u sshhub -f`.

**Backups**: a systemd timer snapshots the database every night with `VACUUM INTO` (safe while the hub runs), checks the copy's integrity, keeps the last 14 in `/var/lib/sshhub/backups`, and copies the host key next to them. Losing the host key would make every visitor's SSH warn that the server's identity changed. Snapshots on the same disk don't survive losing the server, so pull one now and then. Snapshots hold chat messages for up to 14 days past the 30 the hub keeps them.

## Adding content

- **Trivia**: edit `assets/questions.json` (`answer` is the index into `choices`; choices are shuffled at runtime).
- **Memes**: drop `.txt` files of classic ASCII art into `assets/memes/`. A leading `NN-` sets the order. The tests keep them to plain ASCII, at most 70 columns by 20 lines.

Assets are embedded into the binary, so rebuild after changes.

## Screenshots

`scripts/screenshots/screenshots.sh` regenerates the images in `docs/screenshots/`. It runs a demo server with seeded players and chat bots, and records a scripted SSH session with [VHS](https://github.com/charmbracelet/vhs) in headless Chrome, so nothing opens on your desktop. Needs `vhs`, `ttyd`, `ffmpeg` and Chrome or Chromium.
