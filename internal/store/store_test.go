package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
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
