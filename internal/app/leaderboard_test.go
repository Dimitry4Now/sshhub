package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"sshhub/internal/store"
)

func TestLeaderboardTabs(t *testing.T) {
	st := newTestStore(t)
	st.Register("SHA256:ada", "ada")
	st.Register("SHA256:linus", "linus")
	st.AddScore("SHA256:ada", triviaGame, 150)
	st.AddScore("SHA256:linus", triviaGame, 90)
	st.AddScore("SHA256:linus", snakeGame, 40)

	s := newLeaderboard(&Deps{Store: st}, &Session{Fingerprint: "SHA256:ada", Nick: "ada"})
	load := s.Init()
	if !strings.Contains(s.View(80, 24), "loading") {
		t.Fatal("no loading state")
	}
	s.Update(result(load))
	view := s.View(80, 24)
	if len(s.rows) != 2 || s.rows[0].Nick != "ada" || !strings.Contains(view, "linus") {
		t.Fatalf("trivia rows %+v", s.rows)
	}

	// A late answer for the tab just left doesn't replace the new tab's rows.
	cmd := s.Update(key(tea.KeyRight))
	s.Update(leaderboardMsg{game: triviaGame, rows: []store.Score{{Nick: "stale"}}})
	if !s.loading {
		t.Fatal("stale trivia result shown on the snake tab")
	}
	s.Update(result(cmd))
	if len(s.rows) != 1 || s.rows[0].Nick != "linus" || s.rows[0].Best != 40 {
		t.Fatalf("snake rows %+v", s.rows)
	}

	s.Update(result(s.Update(key(tea.KeyRight)))) // wraps around to trivia
	if s.tab != 0 || len(s.rows) != 2 {
		t.Fatalf("tab %d rows %+v", s.tab, s.rows)
	}
	if _, ok := result(s.Update(key(tea.KeyEscape))).(backMsg); !ok {
		t.Fatal("esc doesn't go back")
	}
}

func TestLeaderboardEmpty(t *testing.T) {
	s := newLeaderboard(&Deps{Store: newTestStore(t)}, &Session{Nick: "bob", Guest: true})
	s.Update(result(s.Init()))
	if !strings.Contains(s.View(80, 24), "No scores yet") {
		t.Fatal("empty board not explained")
	}
}
