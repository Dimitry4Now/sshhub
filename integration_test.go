//go:build integration

// Integration tests: the whole server, with its real middleware, on a local
// port, driven by an SSH client the way visitors and admins use it. Run with
// go test -tags integration .
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"sshhub/internal/store"
)

// testHub is a running server and what a test needs to reach it.
type testHub struct {
	addr  string
	srv   *server
	store *store.Store
	admin gossh.Signer // listed in the admin keys file
}

func startHub(t *testing.T, change func(*config)) *testHub {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	admin := newSigner(t)
	keys := filepath.Join(dir, "admin_keys")
	if err := os.WriteFile(keys, gossh.MarshalAuthorizedKey(admin.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := defaults
	cfg.hostKey = filepath.Join(dir, "host_ed25519")
	cfg.adminKeys = keys
	cfg.sessionBurst = 100 // every test connects from 127.0.0.1
	if change != nil {
		change(&cfg)
	}
	srv, err := newServer(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() {
		srv.shutdown(time.Second)
		st.Close()
	})
	return &testHub{addr: l.Addr().String(), srv: srv, store: st, admin: admin}
}

func newSigner(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// dial connects with key, or as a guest called user when key is nil.
func (h *testHub) dial(user string, key gossh.Signer) (*gossh.Client, error) {
	auth := []gossh.AuthMethod{gossh.KeyboardInteractive(
		func(string, string, []string, []bool) ([]string, error) { return nil, nil })}
	if key != nil {
		auth = []gossh.AuthMethod{gossh.PublicKeys(key)}
	}
	return gossh.Dial("tcp", h.addr, &gossh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), // a fresh key per test
		Timeout:         5 * time.Second,
	})
}

func (h *testHub) mustDial(t *testing.T, user string, key gossh.Signer) *gossh.Client {
	t.Helper()
	c, err := h.dial(user, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// command runs an admin-style command and returns its output and exit status.
func (h *testHub) command(t *testing.T, key gossh.Signer, cmd string) (stdout, stderr string, status int) {
	t.Helper()
	sess, err := h.mustDial(t, "admin", key).NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var out, errOut bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &errOut
	err = sess.Run(cmd)
	var exit *gossh.ExitError
	switch {
	case errors.As(err, &exit):
		status = exit.ExitStatus()
	case err != nil:
		t.Fatalf("%s: %v", cmd, err)
	}
	return out.String(), errOut.String(), status
}

// terminal is an interactive session, like ssh in a terminal.
type terminal struct {
	sess  *gossh.Session
	keys  interface{ Write([]byte) (int, error) }
	mu    sync.Mutex
	out   bytes.Buffer
	ended chan struct{}
}

func (h *testHub) open(t *testing.T, user string, key gossh.Signer) *terminal {
	t.Helper()
	sess, err := h.mustDial(t, user, key).NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.RequestPty("xterm-256color", 30, 100, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	term := &terminal{sess: sess, keys: stdin, ended: make(chan struct{})}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			term.mu.Lock()
			term.out.Write(buf[:n])
			term.mu.Unlock()
			if err != nil {
				close(term.ended)
				return
			}
		}
	}()
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	return term
}

// waitFor waits until the screen output contains text.
func (term *terminal) waitFor(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		term.mu.Lock()
		found := strings.Contains(term.out.String(), text)
		term.mu.Unlock()
		if found {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	term.mu.Lock()
	defer term.mu.Unlock()
	t.Fatalf("%q never appeared; output:\n%q", text, term.out.String())
}

func (term *terminal) press(t *testing.T, keys string) {
	t.Helper()
	if _, err := term.keys.Write([]byte(keys)); err != nil {
		t.Fatal(err)
	}
}

func (term *terminal) waitEnd(t *testing.T) {
	t.Helper()
	select {
	case <-term.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("session still open")
	}
}

// eventually waits for cond, for state that changes as sessions come and go.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal(what)
}

func online(h *testHub) []string {
	var nicks []string
	for _, p := range h.srv.hub.Online() {
		nicks = append(nicks, p.Nick)
	}
	return nicks
}

func TestGuestVisit(t *testing.T) {
	h := startHub(t, nil)
	term := h.open(t, "mallory", nil)
	term.waitFor(t, "Trivia")
	eventually(t, "guest not online", func() bool {
		n := online(h)
		return len(n) == 1 && n[0] == "mallory (guest)"
	})

	term.press(t, "q")
	term.waitEnd(t)
	eventually(t, "guest still online after quitting", func() bool { return len(online(h)) == 0 })
}

func TestNewKeyPicksNicknameAndIsRemembered(t *testing.T) {
	h := startHub(t, nil)
	key := newSigner(t)
	fp := gossh.FingerprintSHA256(key.PublicKey())

	term := h.open(t, "whoever", key)
	term.waitFor(t, "new face")
	term.press(t, "ada\r")
	term.waitFor(t, "Trivia")
	if p, ok, err := h.store.Profile(fp); !ok || err != nil || p.Nick != "ada" {
		t.Fatalf("profile %+v ok=%v err=%v", p, ok, err)
	}

	// Connecting again with the same key closes the first session and goes
	// straight to the menu.
	again := h.open(t, "whoever", key)
	term.waitFor(t, "somewhere else")
	again.waitFor(t, "ada")
	eventually(t, "both sessions online", func() bool { return len(online(h)) == 1 })
}

func TestChatBetweenTwoVisitors(t *testing.T) {
	h := startHub(t, nil)
	alice := h.open(t, "alice", nil)
	bob := h.open(t, "bob", nil)
	alice.waitFor(t, "Chat lobby")
	bob.waitFor(t, "Chat lobby")

	alice.press(t, "3")
	bob.press(t, "3")
	alice.waitFor(t, "leave chat")
	bob.waitFor(t, "leave chat")
	alice.press(t, "hello from alice\r")
	bob.waitFor(t, "hello from alice")

	// Posted messages are saved for the next start.
	eventually(t, "message not saved", func() bool {
		msgs, _ := h.store.RecentMessages(10)
		return len(msgs) == 1 && msgs[0].Text == "hello from alice"
	})
}

func TestAdminCommandsAndRefusals(t *testing.T) {
	h := startHub(t, nil)
	out, _, status := h.command(t, h.admin, "stats")
	if status != 0 || !strings.Contains(out, "version") || !strings.Contains(out, "connections") {
		t.Fatalf("stats: %d %q", status, out)
	}

	_, errOut, status := h.command(t, newSigner(t), "who")
	if status != 1 || !strings.Contains(errOut, "no commands") {
		t.Fatalf("stranger: %d %q", status, errOut)
	}

	out, _, _ = h.command(t, h.admin, "audit")
	if !strings.Contains(out, "stats") || strings.Contains(out, "who") {
		t.Fatalf("audit should list the admin's command only: %q", out)
	}
}

func TestBannedVisitorIsTurnedAway(t *testing.T) {
	h := startHub(t, nil)
	guest := h.open(t, "troll", nil)
	guest.waitFor(t, "Trivia")

	// Guests are banned by session ID: their name is anyone's to take.
	if _, errOut, status := h.command(t, h.admin, "ban troll 1h"); status != 1 || !strings.Contains(errOut, "session ID") {
		t.Fatalf("ban by a guest's name: %d %q", status, errOut)
	}
	who, _, _ := h.command(t, h.admin, "who")
	id := strings.Fields(strings.Split(strings.TrimSpace(who), "\n")[1])[0]
	out, _, status := h.command(t, h.admin, "ban "+id+" 1h")
	if status != 0 || !strings.Contains(out, "host 127.0.0.1") || !strings.Contains(out, "closed 1") {
		t.Fatalf("ban: %d %q", status, out)
	}
	guest.waitFor(t, "An admin closed this session")
	guest.waitEnd(t)

	// Coming back, with or without a key, from the banned address.
	for _, key := range []gossh.Signer{nil, newSigner(t)} {
		term := h.open(t, "troll", key)
		term.waitFor(t, "You're banned from this hub until")
		term.waitEnd(t)
	}
	if n := online(h); len(n) != 0 {
		t.Fatalf("banned visitors reached the hub: %v", n)
	}

	// The admin isn't locked out, even from the banned address.
	if out, _, status := h.command(t, h.admin, "unban 1"); status != 0 || !strings.Contains(out, "lifted") {
		t.Fatalf("unban: %d %q", status, out)
	}
	h.open(t, "troll", nil).waitFor(t, "Trivia")
}

func TestConnectionCapPerAddress(t *testing.T) {
	h := startHub(t, func(c *config) { c.maxConnsPerIP = 2 })
	h.mustDial(t, "one", nil)
	h.mustDial(t, "two", nil)
	if c, err := h.dial("three", nil); err == nil {
		c.Close()
		t.Fatal("third connection from one address admitted")
	}
}

func TestNewSessionsAreRateLimited(t *testing.T) {
	h := startHub(t, func(c *config) { c.sessionBurst, c.sessionEvery = 2, time.Hour })
	for range 2 {
		if _, _, status := h.command(t, h.admin, "help"); status != 0 {
			t.Fatalf("session within the burst refused: %d", status)
		}
	}
	if out, errOut, status := h.command(t, h.admin, "help"); status == 0 {
		t.Fatalf("third session ran: %q %q", out, errOut)
	}
}

func TestShutdownTellsEveryone(t *testing.T) {
	h := startHub(t, nil)
	term := h.open(t, "alice", nil)
	term.waitFor(t, "Trivia")
	go h.srv.shutdown(5 * time.Second)
	term.waitFor(t, "The hub is restarting")
	term.waitEnd(t)
}
