package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestRegisterAndProfile(t *testing.T) {
	st := open(t)
	if _, ok, err := st.Profile("fp1"); err != nil || ok {
		t.Fatalf("unknown key: ok=%v err=%v", ok, err)
	}
	if err := st.Register("fp1", "ada"); err != nil {
		t.Fatal(err)
	}
	if err := st.Register("fp2", "ADA"); !errors.Is(err, ErrNickTaken) {
		t.Fatalf("case-insensitive duplicate: err=%v", err)
	}
	p, ok, err := st.Profile("fp1")
	if err != nil || !ok || p.Nick != "ada" || p.Theme != "" {
		t.Fatalf("profile = %+v ok=%v err=%v", p, ok, err)
	}
}

func TestRenameFollowsLeaderboard(t *testing.T) {
	st := open(t)
	st.Register("fp1", "ada")
	st.Register("fp2", "linus")
	st.AddScore("fp1", "trivia", 500)

	if err := st.Rename("fp1", "Linus"); !errors.Is(err, ErrNickTaken) {
		t.Fatalf("rename to taken nick: err=%v", err)
	}
	if err := st.Rename("fp1", "ADA"); err != nil {
		t.Fatalf("own case change: %v", err)
	}
	if err := st.Rename("fp1", "lovelace"); err != nil {
		t.Fatal(err)
	}
	top, err := st.Top("trivia", 10)
	if err != nil || len(top) != 1 || top[0].Nick != "lovelace" {
		t.Fatalf("top = %+v err=%v", top, err)
	}
}

func TestThemeAndClearScores(t *testing.T) {
	st := open(t)
	st.Register("fp1", "ada")
	st.Register("fp2", "linus")
	if err := st.SetTheme("fp1", "Papaya"); err != nil {
		t.Fatal(err)
	}
	if p, _, _ := st.Profile("fp1"); p.Theme != "Papaya" {
		t.Fatalf("theme = %q", p.Theme)
	}

	st.AddScore("fp1", "trivia", 100)
	st.AddScore("fp1", "snake", 50)
	st.AddScore("fp2", "trivia", 70)
	n, err := st.ClearScores("fp1")
	if err != nil || n != 2 {
		t.Fatalf("cleared %d err=%v", n, err)
	}
	for _, game := range []string{"trivia", "snake"} {
		top, _ := st.Top(game, 10)
		for _, row := range top {
			if row.Nick == "ada" {
				t.Fatalf("ada still on the %s board", game)
			}
		}
	}
	if top, _ := st.Top("trivia", 10); len(top) != 1 {
		t.Fatalf("other players' scores touched: %+v", top)
	}
}

func TestMigratesOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	// The schema as it was before themes existed.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE users (fingerprint TEXT PRIMARY KEY, nick TEXT NOT NULL UNIQUE COLLATE NOCASE, created_at INTEGER NOT NULL);
		INSERT INTO users VALUES ('fp1', 'ada', 0);`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := st.Profile("fp1")
	if err != nil || !ok || p.Nick != "ada" || p.Theme != "" {
		t.Fatalf("profile after migration = %+v ok=%v err=%v", p, ok, err)
	}

	// Opening again must not re-run the migration.
	st.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st2.Close()
}

func TestRecentMessages(t *testing.T) {
	st := open(t)
	if msgs, err := st.RecentMessages(10); err != nil || len(msgs) != 0 {
		t.Fatalf("empty: %v %v", msgs, err)
	}
	start := time.Unix(1_700_000_000, 0)
	for i, text := range []string{"one", "two", "three", "four"} {
		m := Message{Time: start.Add(time.Duration(i) * time.Minute), Nick: "ada", Text: text}
		if err := st.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := st.RecentMessages(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 || msgs[0].Text != "two" || msgs[2].Text != "four" {
		t.Fatalf("want the last three oldest first, got %+v", msgs)
	}
	if !msgs[2].Time.Equal(start.Add(3 * time.Minute)) {
		t.Fatalf("time = %v", msgs[2].Time)
	}
}

func TestAudit(t *testing.T) {
	st := open(t)
	at := time.Unix(1_700_000_000, 0)
	for _, cmd := range []string{"who", "kick bob"} {
		if err := st.Audit(AuditEntry{Time: at, Fingerprint: "SHA256:admin", Command: cmd}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.RecentAudit(10)
	if err != nil || len(got) != 2 || got[0].Command != "kick bob" || got[1].Fingerprint != "SHA256:admin" || !got[0].Time.Equal(at) {
		t.Fatalf("audit = %+v err=%v", got, err)
	}
}

func TestBans(t *testing.T) {
	st := open(t)
	now := time.Unix(1_800_000_000, 0)
	if _, banned, err := st.Banned("SHA256:k", "203.0.113.9", now); banned || err != nil {
		t.Fatalf("banned before any ban: %v %v", banned, err)
	}

	keyBan, err := st.AddBan(Ban{Kind: BanKey, Value: "SHA256:k", Until: now.Add(time.Hour), By: "SHA256:admin", Created: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBan(Ban{Kind: BanHost, Value: "203.0.113.9", Forever: true, By: "SHA256:admin", Created: now}); err != nil {
		t.Fatal(err)
	}

	// The key is out from any address, the host with any key or none.
	if b, banned, _ := st.Banned("SHA256:k", "198.51.100.1", now); !banned || b.ID != keyBan {
		t.Fatalf("key ban: %+v %v", b, banned)
	}
	if b, banned, _ := st.Banned("", "203.0.113.9", now); !banned || !b.Forever {
		t.Fatalf("host ban: %+v %v", b, banned)
	}
	if _, banned, _ := st.Banned("", "198.51.100.1", now); banned {
		t.Fatal("empty fingerprint matched a ban")
	}

	// Time bans run out.
	later := now.Add(2 * time.Hour)
	if _, banned, _ := st.Banned("SHA256:k", "198.51.100.1", later); banned {
		t.Fatal("expired ban still applies")
	}
	if bans, _ := st.Bans(later); len(bans) != 1 || bans[0].Kind != BanHost {
		t.Fatalf("active bans = %+v", bans)
	}

	// Banning again replaces the earlier ban.
	again, _ := st.AddBan(Ban{Kind: BanKey, Value: "SHA256:k", Forever: true, By: "SHA256:admin", Created: later})
	if bans, _ := st.Bans(later); len(bans) != 2 || bans[1].ID != again || !bans[1].Forever {
		t.Fatalf("bans after re-ban = %+v", bans)
	}

	if ok, err := st.Unban(again); !ok || err != nil {
		t.Fatalf("unban: %v %v", ok, err)
	}
	if ok, _ := st.Unban(again); ok {
		t.Fatal("unbanned twice")
	}
	if _, banned, _ := st.Banned("SHA256:k", "198.51.100.1", later); banned {
		t.Fatal("unbanned key still banned")
	}
}

func TestFingerprintOf(t *testing.T) {
	st := open(t)
	st.Register("SHA256:ada", "Ada")
	if fp, ok, err := st.FingerprintOf("ada"); !ok || err != nil || fp != "SHA256:ada" {
		t.Fatalf("got %q %v %v", fp, ok, err)
	}
	if _, ok, err := st.FingerprintOf("nobody"); ok || err != nil {
		t.Fatalf("unknown nick: %v %v", ok, err)
	}
}
