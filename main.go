// Command sshhub serves a hangout TUI (trivia, memes, leaderboard) over SSH.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"charm.land/log/v2"
	"charm.land/ssh"

	"sshhub/internal/backup"
	"sshhub/internal/store"
)

func main() {
	cfg := defaults
	flag.StringVar(&cfg.addr, "addr", cfg.addr, "listen address")
	dbPath := flag.String("db", "hub.db", "SQLite database path")
	flag.StringVar(&cfg.hostKey, "hostkey", cfg.hostKey, "host key path (created if missing)")
	flag.BoolVar(&cfg.dev, "dev", false, "allow several sessions per person (for local testing)")
	flag.IntVar(&cfg.maxConns, "max-conns", cfg.maxConns, "most connections open at once")
	flag.IntVar(&cfg.maxConnsPerIP, "max-conns-per-ip", cfg.maxConnsPerIP, "most connections open at once from one address")
	showVersion := flag.Bool("version", false, "print the version and exit")
	backupDir := flag.String("backup", "", "snapshot the database and host key into this directory and exit")
	keep := flag.Int("keep", 14, "with -backup: how many snapshots to keep")
	flag.StringVar(&cfg.adminKeys, "admin-keys", "", "authorized_keys file of keys that may run admin commands")
	chatDays := flag.Int("chat-days", 30, "delete chat messages older than this many days (0 keeps them)")
	flag.Parse()

	if *showVersion {
		fmt.Println(version())
		return
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatal("open database", "err", err)
	}
	defer st.Close()

	if *backupDir != "" {
		path, err := backup.Snapshot(st, *backupDir, *keep, time.Now())
		if err != nil {
			log.Fatal("backup", "err", err)
		}
		if err := backup.CopyFile(cfg.hostKey, *backupDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal("back up host key", "err", err)
		}
		log.Info("backup written", "path", path)
		return
	}

	// Prune before the server loads the history, so expired messages
	// aren't shown.
	pruneChat(st, *chatDays)
	srv, err := newServer(cfg, st)
	if err != nil {
		log.Fatal("create server", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Info("starting ssh hub", "addr", cfg.addr, "version", version())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			stop()
		}
	}()
	if *chatDays > 0 {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Hour):
					pruneChat(st, *chatDays)
				}
			}
		}()
	}
	<-ctx.Done()
	srv.shutdown(5 * time.Second)
	log.Info("stopped")
}

// pruneChat deletes chat messages older than days; 0 keeps them all.
func pruneChat(st *store.Store, days int) {
	if days <= 0 {
		return
	}
	n, err := st.PruneMessages(time.Now().AddDate(0, 0, -days))
	if err != nil {
		log.Error("prune chat", "err", err)
	} else if n > 0 {
		log.Info("pruned chat", "messages", n, "days", days)
	}
}

// release is the release tag of the build, set by the deploy:
// go build -ldflags "-X main.release=v1.2.3"
var release string

// version describes the build: the release tag if there is one, then the VCS
// info Go embeds: the commit, its date, and "-dirty" when built with
// uncommitted changes.
func version() string {
	v := commit()
	if release != "" {
		v = release + " · " + v
	}
	return v
}

func commit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var rev, at string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			at = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return info.Main.Version
	}
	v := rev[:min(len(rev), 7)]
	if dirty {
		v += "-dirty"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		v += " (" + t.Format("2006-01-02") + ")"
	}
	return v
}
