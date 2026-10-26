// Package backup snapshots the hub's state and prunes old snapshots.
package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"sshhub/internal/store"
)

const (
	prefix = "hub-"
	suffix = ".db"
)

// Snapshot writes a copy of the database into dir, named after now, checks
// that the copy is intact, and deletes all but the newest keep snapshots. It
// returns the new snapshot's path.
func Snapshot(st *store.Store, dir string, keep int, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, prefix+now.UTC().Format("20060102T150405Z")+suffix)
	// Work on a temporary name so a half-written or broken copy never looks
	// like a snapshot.
	tmp := path + ".tmp"
	os.Remove(tmp)
	if err := st.Backup(tmp); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("copy: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil { // it holds user data
		os.Remove(tmp)
		return "", err
	}
	if err := check(tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, prune(dir, keep)
}

func check(path string) error {
	cp, err := store.Open(path)
	if err != nil {
		return fmt.Errorf("open copy: %w", err)
	}
	defer cp.Close()
	return cp.Check()
}

// prune deletes the oldest snapshots beyond keep. Names sort by time.
func prune(dir string, keep int) error {
	names, err := filepath.Glob(filepath.Join(dir, prefix+"*"+suffix))
	if err != nil {
		return err
	}
	sort.Strings(names)
	for len(names) > keep {
		if err := os.Remove(names[0]); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}

// CopyFile copies src into dir under the same name, readable only by the
// owner. It is used for the host key, which never changes but must survive
// a lost server.
func CopyFile(src, dir string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filepath.Base(src)), data, 0o600)
}
