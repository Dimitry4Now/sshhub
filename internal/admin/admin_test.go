package admin

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strconv"
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

func TestBanLength(t *testing.T) {
	cases := map[string]time.Duration{"30m": 30 * time.Minute, "12h": 12 * time.Hour, "7d": 7 * 24 * time.Hour}
	for in, want := range cases {
		if d, forever, err := banLength(in); d != want || forever || err != nil {
			t.Errorf("banLength(%q) = %v %v %v", in, d, forever, err)
		}
	}
	if _, forever, err := banLength("forever"); !forever || err != nil {
		t.Errorf("forever: %v %v", forever, err)
	}
	for _, bad := range []string{"", "0d", "-1h", "0s", "soon", "1.5d"} {
		if _, _, err := banLength(bad); err == nil {
			t.Errorf("banLength(%q) accepted", bad)
		}
	}
}

func TestBanSession(t *testing.T) {
	a, fp := setup(t)
	kicked := make(chan hub.KickReason, 2)
	kick := func(r hub.KickReason) { kicked <- r }
	troll := a.Hub.Connect(hub.Client{Identity: "key:SHA256:troll", Nick: "troll", Addr: "203.0.113.5:4000", Kick: kick})
	// A second session from the same address goes too, a session elsewhere stays.
	a.Hub.Connect(hub.Client{Identity: "guest:203.0.113.5:alt", Nick: "alt", Guest: true, Addr: "203.0.113.5:4001", Kick: kick})
	a.Hub.Connect(hub.Client{Identity: "key:SHA256:bob", Nick: "bob", Addr: "198.51.100.7:22"})

	code, out, errOut := run(a, fp, "ban", strconv.Itoa(troll), "7d")
	if code != 0 || !strings.Contains(out, "key SHA256:troll") || !strings.Contains(out, "host 203.0.113.5") ||
		!strings.Contains(out, "closed 2 session(s)") {
		t.Fatalf("ban: %d %q %q", code, out, errOut)
	}
	for range 2 {
		<-kicked
	}
	if a.Hub.Count() != 1 {
		t.Fatalf("count = %d", a.Hub.Count())
	}

	// The key is out from anywhere, the address with any key or none.
	if msg := a.refusal("SHA256:troll", "192.0.2.1:1", time.Now()); !strings.Contains(msg, "banned") || !strings.Contains(msg, "until") {
		t.Fatalf("key refusal %q", msg)
	}
	if a.refusal("", "203.0.113.5:9999", time.Now()) == "" {
		t.Fatal("guest from banned address let in")
	}
	if a.refusal("SHA256:troll", "192.0.2.1:1", time.Now().Add(8*24*time.Hour)) != "" {
		t.Fatal("ban did not run out")
	}
	if a.refusal("SHA256:bob", "198.51.100.7:22", time.Now()) != "" {
		t.Fatal("bystander refused")
	}

	code, out, _ = run(a, fp, "bans")
	if code != 0 || strings.Count(out, "\n") != 3 || !strings.Contains(out, fp) {
		t.Fatalf("bans: %d %q", code, out)
	}
	bans, _ := a.Store.Bans(time.Now())
	for _, b := range bans {
		if code, _, _ := run(a, fp, "unban", strconv.FormatInt(b.ID, 10)); code != 0 {
			t.Fatalf("unban %d: %d", b.ID, code)
		}
	}
	if a.refusal("SHA256:troll", "203.0.113.5:1", time.Now()) != "" {
		t.Fatal("still refused after unban")
	}
	if code, _, _ := run(a, fp, "unban", "999"); code != 1 {
		t.Fatalf("unban unknown: %d", code)
	}
}

func TestBanTargets(t *testing.T) {
	a, fp := setup(t)
	a.Store.Register("SHA256:offline", "Ghost")
	a.Hub.Connect(hub.Client{Identity: "guest:198.51.100.2:ghost", Nick: "ghost", Guest: true, Addr: "198.51.100.2:5"})

	// A registered nickname that isn't online: its key only, and the guest
	// calling themselves the same isn't touched.
	code, out, _ := run(a, fp, "ban", "ghost", "forever")
	if code != 0 || !strings.Contains(out, "key SHA256:offline forever") || strings.Contains(out, "host") ||
		!strings.Contains(out, "closed 0") {
		t.Fatalf("ban offline nick: %d %q", code, out)
	}

	// An IPv6 address bans its /64.
	code, out, _ = run(a, fp, "ban", "2001:db8:1:2::99", "1h")
	if code != 0 || !strings.Contains(out, "host 2001:db8:1:2::/64") {
		t.Fatalf("ban IPv6: %d %q", code, out)
	}
	if a.refusal("", "[2001:db8:1:2:abcd::1]:22", time.Now()) == "" {
		t.Fatal("other address in the /64 let in")
	}

	for _, bad := range [][]string{{"ban"}, {"ban", "1.2.3.4", "soon"}, {"unban"}, {"unban", "x"}} {
		if code, _, _ := run(a, fp, bad...); code != 2 {
			t.Fatalf("%v: code %d", bad, code)
		}
	}
	if code, _, errOut := run(a, fp, "ban", "nobody"); code != 1 || !strings.Contains(errOut, "nobody called") {
		t.Fatalf("unknown nick: %d %q", code, errOut)
	}
	if code, _, errOut := run(a, fp, "ban", "42"); code != 1 || !strings.Contains(errOut, "no session 42") {
		t.Fatalf("unknown session: %d %q", code, errOut)
	}
	if code, _, errOut := run(a, fp, "ban", fp); code != 1 || !strings.Contains(errOut, "admin key") {
		t.Fatalf("banning an admin: %d %q", code, errOut)
	}
}
