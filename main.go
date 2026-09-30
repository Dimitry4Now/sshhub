// Command sshhub serves a hangout TUI (trivia, memes, leaderboard) over SSH.
package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"regexp"
	"syscall"
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
	"sshhub/internal/app"
	"sshhub/internal/hub"
	"sshhub/internal/store"
)

func main() {
	addr := flag.String("addr", ":23234", "listen address")
	dbPath := flag.String("db", "hub.db", "SQLite database path")
	hostKey := flag.String("hostkey", ".ssh/id_ed25519", "host key path (created if missing)")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatal("open database", "err", err)
	}
	defer st.Close()

	questions, err := app.LoadQuestions(assets.FS, "questions.json")
	if err != nil {
		log.Fatal("load questions", "err", err)
	}
	memes, err := app.LoadMemes(assets.FS, "memes")
	if err != nil {
		log.Fatal("load memes", "err", err)
	}

	deps := &app.Deps{
		Store:     st,
		Hub:       hub.New(),
		Questions: questions,
		Memes:     memes,
	}

	srv, err := wish.NewServer(
		wish.WithAddress(*addr),
		wish.WithHostKeyPath(*hostKey),
		// Any key is welcome: the key fingerprint is the user's identity.
		wish.WithPublicKeyAuth(func(ssh.Context, ssh.PublicKey) bool { return true }),
		// Clients without a key get in as guests.
		wish.WithKeyboardInteractiveAuth(func(ssh.Context, gossh.KeyboardInteractiveChallenge) bool { return true }),
		wish.WithIdleTimeout(15*time.Minute),
		wish.WithMaxTimeout(3*time.Hour),
		wish.WithMiddleware(
			bubbletea.Middleware(teaHandler(deps)),
			activeterm.Middleware(),
			ratelimiter.Middleware(ratelimiter.NewRateLimiter(rate.Every(2*time.Second), 5, 4096)),
			logging.Middleware(),
		),
	)
	if err != nil {
		log.Fatal("create server", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Info("starting ssh hub", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			stop()
		}
	}()
	<-ctx.Done()

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("shutdown", "err", err)
	}
}

var guestNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func teaHandler(deps *app.Deps) bubbletea.Handler {
	return func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		sess := &app.Session{}
		if key := s.PublicKey(); key != nil {
			sess.Fingerprint = gossh.FingerprintSHA256(key)
			nick, err := deps.Store.Nick(sess.Fingerprint)
			if err != nil {
				log.Error("lookup nick", "err", err)
			}
			sess.Nick = nick
		} else {
			sess.Guest = true
			sess.Nick = guestNameRe.ReplaceAllString(s.User(), "")
			if len(sess.Nick) > 16 {
				sess.Nick = sess.Nick[:16]
			}
			if sess.Nick == "" {
				sess.Nick = "guest"
			}
		}
		sess.HubID = deps.Hub.Connect(sess.Nick, sess.Guest)
		go func() {
			<-s.Context().Done()
			deps.Hub.Disconnect(sess.HubID)
		}()
		return app.New(deps, sess), bubbletea.MakeOptions(s)
	}
}
