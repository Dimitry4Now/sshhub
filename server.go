package main

import (
	"context"
	"errors"
	"net"
	"regexp"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	"charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"charm.land/wish/v2/ratelimiter"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"

	"sshhub/assets"
	"sshhub/internal/admin"
	"sshhub/internal/app"
	"sshhub/internal/hub"
	"sshhub/internal/limit"
	"sshhub/internal/store"
)

// config holds the server's settings, from the flags.
type config struct {
	addr          string
	hostKey       string
	adminKeys     string
	dev           bool
	maxConns      int
	maxConnsPerIP int
	// New sessions from one address: sessionBurst at once, then one per
	// sessionEvery.
	sessionEvery time.Duration
	sessionBurst int
}

// defaults are the settings without flags; main's flags start from them.
var defaults = config{
	addr:          ":23234",
	hostKey:       ".ssh/id_ed25519",
	maxConns:      100,
	maxConnsPerIP: 10,
	sessionEvery:  2 * time.Second,
	sessionBurst:  5,
}

// server is the SSH server and the hub every session shares.
type server struct {
	*ssh.Server
	hub      *hub.Hub
	archived chan struct{} // closed once every chat message is saved
	stopping sync.Once
}

// newServer sets up the hub on st: chat history, the archive that saves new
// messages, and the SSH server with its middleware. Start it with
// ListenAndServe or Serve, stop it with shutdown.
func newServer(cfg config, st *store.Store) (*server, error) {
	saved, err := st.RecentMessages(100)
	if err != nil {
		return nil, err
	}
	history := make([]hub.Message, len(saved))
	for i, m := range saved {
		history[i] = hub.Message{Time: m.Time, Nick: m.Nick, Text: m.Text}
	}
	h := hub.New(history)

	questions, err := app.LoadQuestions(assets.FS, "questions.json")
	if err != nil {
		return nil, err
	}
	memes, err := app.LoadMemes(assets.FS, "memes")
	if err != nil {
		return nil, err
	}
	deps := &app.Deps{
		Store:     st,
		Hub:       h,
		Questions: questions,
		Memes:     memes,
		Version:   version(),
	}

	inHub := &sync.Map{} // ssh.Session -> hub id, see leaveHub
	conns := limit.NewConns(cfg.maxConns, cfg.maxConnsPerIP)
	adm := &admin.Admin{
		KeysPath: cfg.adminKeys,
		Hub:      h,
		Store:    st,
		Conns:    conns.Open,
		Version:  deps.Version,
		Started:  time.Now(),
	}

	srv, err := wish.NewServer(
		wish.WithAddress(cfg.addr),
		wish.WithHostKeyPath(cfg.hostKey),
		// Any key is welcome: the key fingerprint is the user's identity.
		wish.WithPublicKeyAuth(func(ssh.Context, ssh.PublicKey) bool { return true }),
		// Clients without a key get in as guests.
		wish.WithKeyboardInteractiveAuth(func(ssh.Context, gossh.KeyboardInteractiveChallenge) bool { return true }),
		// For sessions without the TUI (admin commands, stalled clients).
		// The TUI redraws every second, which counts as activity here, so
		// it closes sessions nobody types in itself.
		wish.WithIdleTimeout(15*time.Minute),
		wish.WithMaxTimeout(3*time.Hour),
		ssh.WrapConn(func(_ ssh.Context, c net.Conn) net.Conn {
			admitted := conns.Admit(c)
			if admitted == nil {
				log.Warn("connection refused: too many open", "remote", c.RemoteAddr(), "open", conns.Open())
			}
			return admitted
		}),
		wish.WithMiddleware(
			bubbletea.MiddlewareWithProgramHandler(programHandler(deps, cfg.dev, inHub)),
			leaveHub(h, inHub),
			activeterm.Middleware(),
			// With 100 connections at this size the hub peaked at about
			// 1.1 GB, inside the service's memory limit.
			limit.Window(300, 100),
			// Turns banned visitors away before they reach the hub.
			adm.Gate(),
			// Runs before the ones above: sessions with a command (ssh host
			// who) are admin commands and never reach the TUI, so a ban
			// can't lock an admin out.
			adm.Middleware(),
			ratelimiter.Middleware(ratelimiter.NewRateLimiter(rate.Every(cfg.sessionEvery), cfg.sessionBurst, 4096)),
			logging.Middleware(),
		),
	)
	if err != nil {
		return nil, err
	}

	// Save chat messages as they're posted, off the chat's hot path. The loop
	// ends when the hub shuts down and closes the archive.
	archived := make(chan struct{})
	go func() {
		defer close(archived)
		for m := range h.Archive() {
			if err := st.SaveMessage(store.Message{Time: m.Time, Nick: m.Nick, Text: m.Text}); err != nil {
				log.Error("save chat message", "err", err)
			}
		}
	}()
	return &server{Server: srv, hub: h, archived: archived}, nil
}

// shutdown tells every session the hub is restarting and gives them grace to
// show it and quit on their own; closing the server cuts the rest off. It
// returns once every chat message is saved, so the database can close.
// Calls after the first wait for it to finish.
func (s *server) shutdown(grace time.Duration) {
	s.stopping.Do(func() { s.stop(grace) })
}

func (s *server) stop(grace time.Duration) {
	log.Info("shutting down", "sessions", s.hub.Count())
	s.hub.Shutdown()
	select {
	case <-s.hub.Drained():
	case <-time.After(grace):
		log.Warn("sessions still open after grace period", "sessions", s.hub.Count())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("shutdown", "err", err)
	}
	<-s.archived
}

var guestNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// leaveHub takes a session out of the hub once its program has ended. The
// session's context would be too late: it lasts as long as the SSH
// connection, which a client can keep open after the session ends, leaving
// a ghost in the online list.
func leaveHub(h *hub.Hub, inHub *sync.Map) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			next(s)
			if id, ok := inHub.LoadAndDelete(s); ok {
				h.Disconnect(id.(int))
			}
		}
	}
}

// programHandler starts the app for a session and puts it in the hub, noting
// the hub id in inHub for leaveHub.
func programHandler(deps *app.Deps, dev bool, inHub *sync.Map) bubbletea.ProgramHandler {
	return func(s ssh.Session) *tea.Program {
		sess := &app.Session{}
		var identity string
		if key := s.PublicKey(); key != nil {
			sess.Fingerprint = gossh.FingerprintSHA256(key)
			identity = "key:" + sess.Fingerprint
			profile, _, err := deps.Store.Profile(sess.Fingerprint)
			if err != nil {
				log.Error("load profile", "err", err)
			}
			sess.Nick, sess.Theme = profile.Nick, profile.Theme
		} else {
			sess.Guest = true
			sess.Nick = guestNameRe.ReplaceAllString(s.User(), "")
			if len(sess.Nick) > 16 {
				sess.Nick = sess.Nick[:16]
			}
			if sess.Nick == "" {
				sess.Nick = "guest"
			}
			// Guests are told apart by address and username, so people
			// sharing a router don't kick each other out.
			host, _, _ := net.SplitHostPort(s.RemoteAddr().String())
			identity = "guest:" + host + ":" + sess.Nick
		}
		if dev {
			identity = ""
		}

		// Signals are for the whole server (see main), not for each session.
		opts := append(bubbletea.MakeOptions(s), tea.WithoutSignalHandler())
		p := tea.NewProgram(app.New(deps, sess), opts...)
		sess.HubID = deps.Hub.Connect(hub.Client{
			Identity: identity,
			Nick:     sess.Nick,
			Guest:    sess.Guest,
			Addr:     s.RemoteAddr().String(),
			Kick:     func(r hub.KickReason) { p.Send(app.KickedMsg{Reason: r}) },
			Notify:   func(text string) { p.Send(app.NoticeMsg{Text: text}) },
		})
		inHub.Store(s, sess.HubID)
		return p
	}
}
