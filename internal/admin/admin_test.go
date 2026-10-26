package admin

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"sshhub/internal/hub"
	"sshhub/internal/store"
)

func newKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// setup returns an Admin whose keys file lists admin, plus that key's
// fingerprint.
func setup(t *testing.T) (*Admin, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	admin := newKey(t)
	keys := "# admins\n\nnot a key line\n" + string(gossh.MarshalAuthorizedKey(admin))
	path := filepath.Join(dir, "admin_keys")
	if err := os.WriteFile(path, []byte(keys), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Admin{KeysPath: path, Hub: hub.New(nil), Store: st, Version: "test", Started: time.Now()}
	return a, gossh.FingerprintSHA256(admin)
}

func run(a *Admin, fp string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = a.Handle(fp, "127.0.0.1:1", args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestOnlyListedKeysAreAdmins(t *testing.T) {
	a, fp := setup(t)
	if ok, err := a.IsAdmin(fp); !ok || err != nil {
		t.Fatalf("listed key: ok=%v err=%v", ok, err)
	}
	stranger := gossh.FingerprintSHA256(newKey(t))
	for _, f := range []string{stranger, ""} {
		if ok, _ := a.IsAdmin(f); ok {
			t.Fatalf("%q is admin", f)
		}
	}

	// Refused commands do nothing and aren't audited as run.
	a.Hub.Connect(hub.Client{Nick: "bob"})
	code, out, errOut := run(a, stranger, "kick", "bob")
	if code == 0 || out != "" || !strings.Contains(errOut, "no commands") || a.Hub.Count() != 1 {
		t.Fatalf("stranger: code=%d out=%q err=%q", code, out, errOut)
	}
	if entries, _ := a.Store.RecentAudit(10); len(entries) != 0 {
		t.Fatalf("refused command audited: %+v", entries)
	}

	// Removing the key from the file applies without a restart.
	os.WriteFile(a.KeysPath, nil, 0o644)
	if ok, _ := a.IsAdmin(fp); ok {
		t.Fatal("key still admin after removal")
	}
	a.KeysPath = filepath.Join(t.TempDir(), "missing")
	if ok, err := a.IsAdmin(fp); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
}

func TestCommands(t *testing.T) {
	a, fp := setup(t)
	notified := make(chan string, 1)
	kicked := make(chan hub.KickReason, 1)
	a.Hub.Connect(hub.Client{Nick: "Bob", Addr: "10.1.1.1:22", Notify: func(s string) { notified <- s },
		Kick: func(r hub.KickReason) { kicked <- r }})

	if code, out, _ := run(a, fp, "who"); code != 0 || !strings.Contains(out, "Bob") || !strings.Contains(out, "10.1.1.1:22") {
		t.Fatalf("who: %d %q", code, out)
	}
	if code, out, _ := run(a, fp, "broadcast", "back", "in", "5"); code != 0 || !strings.Contains(out, "sent to 1") || <-notified != "back in 5" {
		t.Fatalf("broadcast: %d %q", code, out)
	}
	if code, out, _ := run(a, fp, "stats"); code != 0 || !strings.Contains(out, "sessions") || !strings.Contains(out, "test") {
		t.Fatalf("stats: %d %q", code, out)
	}
	if code, _, _ := run(a, fp, "kick", "nobody"); code != 1 {
		t.Fatalf("kick unknown: %d", code)
	}
	if code, out, _ := run(a, fp, "kick", "bob"); code != 0 || !strings.Contains(out, "kicked 1") || <-kicked != hub.KickAdmin {
		t.Fatalf("kick by nick: %d %q", code, out)
	}
	if code, _, errOut := run(a, fp, "rm", "-rf"); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unknown: %d %q", code, errOut)
	}

	// Every command was recorded, newest first.
	code, out, _ := run(a, fp, "audit", "3")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 || !strings.HasSuffix(lines[0], "audit 3") || !strings.HasSuffix(lines[1], "rm -rf") {
		t.Fatalf("audit: %d %q", code, out)
	}
}

func TestCRLF(t *testing.T) {
	var b bytes.Buffer
	n, err := crlf{&b}.Write([]byte("a\nb\n"))
	if err != nil || n != 4 || b.String() != "a\r\nb\r\n" {
		t.Fatalf("n=%d err=%v %q", n, err, b.String())
	}
}
