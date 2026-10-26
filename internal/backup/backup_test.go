package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"sshhub/internal/store"
)

func TestSnapshotRestoresAndPrunes(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Register("fp1", "ada"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddScore("fp1", "snake", 120); err != nil {
		t.Fatal(err)
	}

	backups := filepath.Join(dir, "backups")
	start := time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC)
	var paths []string
	for i := range 4 {
		p, err := Snapshot(st, backups, 3, start.Add(time.Duration(i)*24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	// The oldest snapshot is pruned, the newest three stay.
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("oldest snapshot not pruned: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(backups, "*"))
	if len(left) != 3 {
		t.Fatalf("backups dir has %v", left)
	}

	// A snapshot is a complete database: restore drill.
	restored, err := store.Open(paths[3])
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	p, ok, err := restored.Profile("fp1")
	if err != nil || !ok || p.Nick != "ada" {
		t.Fatalf("restored profile = %+v ok=%v err=%v", p, ok, err)
	}
	top, err := restored.Top("snake", 10)
	if err != nil || len(top) != 1 || top[0].Best != 120 {
		t.Fatalf("restored scores = %+v err=%v", top, err)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "host_ed25519")
	if err := os.WriteFile(src, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o700)
	if err := CopyFile(src, out); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(out, "host_ed25519"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("copy: %v %v", fi, err)
	}
}

func TestSnapshotIsPrivate(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, err := Snapshot(st, filepath.Join(dir, "b"), 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode: %v %v", fi.Mode(), err)
	}
}
