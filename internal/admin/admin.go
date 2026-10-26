// Package admin runs commands sent over SSH (ssh -p PORT host who) for the
// keys listed in the admin keys file.
//
// The file is in authorized_keys format and belongs to root on the server;
// nothing inside the hub can change who is an admin.
package admin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"charm.land/log/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	gossh "golang.org/x/crypto/ssh"

	"sshhub/internal/hub"
	"sshhub/internal/store"
)

// Admin holds what the commands look at.
type Admin struct {
	KeysPath string // admin keys file; empty or missing means no admins
	Hub      *hub.Hub
	Store    *store.Store
	Conns    func() int // open connections
	Version  string
	Started  time.Time
}

// IsAdmin reports whether a key fingerprint is listed in the keys file. The
// file is read on every call, so adding or removing a key applies at once.
func (a *Admin) IsAdmin(fingerprint string) (bool, error) {
	if a.KeysPath == "" || fingerprint == "" {
		return false, nil
	}
	data, err := os.ReadFile(a.KeysPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for len(bytes.TrimSpace(data)) > 0 {
		key, _, _, rest, err := gossh.ParseAuthorizedKey(data)
		if err != nil {
			// Only unparseable lines are left (ParseAuthorizedKey skips
			// comments and bad lines while it can find a key).
			break
		}
		if gossh.FingerprintSHA256(key) == fingerprint {
			return true, nil
		}
		data = rest
	}
	return false, nil
}

// Middleware handles sessions that ask for a command. Sessions without one
// (people coming to play) go on to next.
func (a *Admin) Middleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			args := s.Command()
			if len(args) == 0 {
				next(s)
				return
			}
			var fp string
			if key := s.PublicKey(); key != nil {
				fp = gossh.FingerprintSHA256(key)
			}
			var out, errOut io.Writer = s, s.Stderr()
			if _, _, isPty := s.Pty(); isPty {
				// A terminal in raw mode needs explicit carriage returns.
				out, errOut = crlf{s}, crlf{s.Stderr()}
			}
			s.Exit(a.Handle(fp, s.RemoteAddr().String(), args, out, errOut))
		}
	}
}

// Handle runs one command for the key with the given fingerprint (empty for
// keyless sessions) and returns the exit status.
func (a *Admin) Handle(fingerprint, addr string, args []string, stdout, stderr io.Writer) int {
	command := strings.Join(args, " ")
	ok, err := a.IsAdmin(fingerprint)
	if err != nil {
		log.Error("read admin keys", "err", err)
		fmt.Fprintln(stderr, "admin keys unavailable")
		return 1
	}
	if !ok {
		log.Warn("command refused", "fingerprint", fingerprint, "remote", addr, "command", command)
		fmt.Fprintln(stderr, "This hub takes no commands. To play, connect without one.")
		return 1
	}
	// No unrecorded admin actions: if the audit log can't be written, the
	// command doesn't run.
	if err := a.Store.Audit(store.AuditEntry{Time: time.Now(), Fingerprint: fingerprint, Command: command}); err != nil {
		log.Error("write audit log", "err", err)
		fmt.Fprintln(stderr, "audit log unavailable; not running the command")
		return 1
	}
	log.Info("admin command", "fingerprint", fingerprint, "remote", addr, "command", command)
	return a.run(args, stdout, stderr)
}

const help = `commands:
  who                 list connected sessions
  kick ID|NICK        close a session (IDs from who)
  broadcast TEXT      show an announcement to everyone
  stats               server and runtime numbers
  audit [N]           the last N admin commands (default 20)
  help                this list
`

func (a *Admin) run(args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "help":
		fmt.Fprint(stdout, help)
	case "who":
		a.who(stdout)
	case "kick":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: kick ID|NICK")
			return 2
		}
		return a.kick(args[1], stdout, stderr)
	case "broadcast":
		text := strings.Join(args[1:], " ")
		if strings.TrimSpace(text) == "" {
			fmt.Fprintln(stderr, "usage: broadcast TEXT")
			return 2
		}
		fmt.Fprintf(stdout, "sent to %d sessions\n", a.Hub.Announce(text))
	case "stats":
		a.stats(stdout)
	case "audit":
		n := 20
		if len(args) > 1 {
			var err error
			if n, err = strconv.Atoi(args[1]); err != nil || n < 1 {
				fmt.Fprintln(stderr, "usage: audit [N]")
				return 2
			}
		}
		return a.audit(n, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], help)
		return 2
	}
	return 0
}

func (a *Admin) who(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNICK\tWHERE\tFOR\tFROM\tIDENTITY")
	for _, s := range a.Hub.Sessions() {
		where := "browsing"
		if s.InChat {
			where = "chat"
		}
		identity := s.Identity
		if strings.HasPrefix(identity, "guest:") {
			identity = "guest"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Nick, where,
			time.Since(s.Since).Round(time.Second), s.Addr, strings.TrimPrefix(identity, "key:"))
	}
	tw.Flush()
}

func (a *Admin) kick(target string, stdout, stderr io.Writer) int {
	if id, err := strconv.Atoi(target); err == nil {
		if !a.Hub.Kick(id) {
			fmt.Fprintf(stderr, "no session %d\n", id)
			return 1
		}
		fmt.Fprintf(stdout, "kicked session %d\n", id)
		return 0
	}
	kicked := 0
	for _, s := range a.Hub.Sessions() {
		if strings.EqualFold(strings.TrimSuffix(s.Nick, " (guest)"), target) && a.Hub.Kick(s.ID) {
			kicked++
		}
	}
	if kicked == 0 {
		fmt.Fprintf(stderr, "nobody called %q is connected\n", target)
		return 1
	}
	fmt.Fprintf(stdout, "kicked %d session(s) of %s\n", kicked, target)
	return 0
}

func (a *Admin) stats(w io.Writer) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	inChat := 0
	sessions := a.Hub.Sessions()
	for _, s := range sessions {
		if s.InChat {
			inChat++
		}
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "version\t%s\n", a.Version)
	fmt.Fprintf(tw, "uptime\t%s\n", time.Since(a.Started).Round(time.Second))
	fmt.Fprintf(tw, "sessions\t%d (%d in chat)\n", len(sessions), inChat)
	if a.Conns != nil {
		fmt.Fprintf(tw, "connections\t%d\n", a.Conns())
	}
	fmt.Fprintf(tw, "goroutines\t%d\n", runtime.NumGoroutine())
	fmt.Fprintf(tw, "heap\t%.1f MiB\n", float64(mem.HeapAlloc)/(1<<20))
	fmt.Fprintf(tw, "from os\t%.1f MiB\n", float64(mem.Sys)/(1<<20))
	fmt.Fprintf(tw, "gc runs\t%d\n", mem.NumGC)
	tw.Flush()
}

func (a *Admin) audit(n int, stdout, stderr io.Writer) int {
	entries, err := a.Store.RecentAudit(n)
	if err != nil {
		fmt.Fprintln(stderr, "read audit log:", err)
		return 1
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Time.UTC().Format("2006-01-02 15:04:05Z"), e.Fingerprint, e.Command)
	}
	tw.Flush()
	return 0
}

// crlf turns "\n" into "\r\n" for sessions with a terminal.
type crlf struct{ w io.Writer }

func (c crlf) Write(p []byte) (int, error) {
	if _, err := c.w.Write(bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))); err != nil {
		return 0, err
	}
	return len(p), nil
}
