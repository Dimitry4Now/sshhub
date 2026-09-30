# sshhub

A hangout spot you reach over SSH: trivia, snake, a chat lobby with an online list, a meme gallery and a leaderboard, with a live clock in the header. No shell access — every connection lands in the TUI.

## Run

```sh
go build -o sshhub .
./sshhub                       # listens on :23234, creates hub.db and .ssh/id_ed25519
ssh -p 23234 localhost         # connect
```

Flags: `-addr`, `-db`, `-hostkey`.

The repo pins a newer Go toolchain in `go.mod`; with Go 1.21+ installed it is downloaded automatically.

## Identity

- Connecting with an SSH key: the key fingerprint is your account. You pick a nickname on first login and scores are saved.
- Connecting without a key (`ssh -o PubkeyAuthentication=no you@host`): guest session, named after your SSH username, scores not saved.

## Adding content

- Trivia: edit `assets/questions.json` (`answer` is the index into `choices`; choices are shuffled at runtime).
- Memes: drop `.txt` or `.ans` files into `assets/memes/`. A leading `NN-` sets the order. Images can be converted with e.g. `chafa --size 60x25 pic.png > assets/memes/07-pic.ans`.

Assets are embedded, so rebuild after changes.
