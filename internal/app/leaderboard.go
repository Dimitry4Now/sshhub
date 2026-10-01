package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"sshhub/internal/store"
)

// boards are the leaderboard tabs.
var boards = []struct{ game, title string }{
	{triviaGame, "Trivia"},
	{snakeGame, "Snake"},
}

type leaderboardMsg struct {
	game string
	rows []store.Score
	err  error
}

type leaderboardScreen struct {
	deps    *Deps
	sess    *Session
	tab     int
	rows    []store.Score
	err     error
	loading bool
}

func newLeaderboard(deps *Deps, sess *Session) *leaderboardScreen {
	return &leaderboardScreen{deps: deps, sess: sess}
}

func (s *leaderboardScreen) Init() tea.Cmd {
	s.loading = true
	st, game := s.deps.Store, boards[s.tab].game
	return func() tea.Msg {
		rows, err := st.Top(game, 10)
		return leaderboardMsg{game, rows, err}
	}
}

func (s *leaderboardScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case leaderboardMsg:
		if msg.game == boards[s.tab].game {
			s.rows, s.err, s.loading = msg.rows, msg.err, false
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			return back
		case "r":
			return s.Init()
		case "right", "l", "tab":
			s.tab = (s.tab + 1) % len(boards)
			return s.Init()
		case "left", "h", "shift+tab":
			s.tab = (s.tab - 1 + len(boards)) % len(boards)
			return s.Init()
		}
	}
	return nil
}

func (s *leaderboardScreen) View(width, height int) string {
	t := theme(s.sess)
	var b strings.Builder
	b.WriteString(t.heading.Render("🏆 Leaderboard") + "\n\n")
	for i, bd := range boards {
		if i == s.tab {
			b.WriteString(t.selected.Render("[ " + bd.title + " ]"))
		} else {
			b.WriteString(t.dim.Render("  " + bd.title + "  "))
		}
		b.WriteString(" ")
	}
	b.WriteString("\n\n")
	switch {
	case s.loading:
		b.WriteString(t.dim.Render("loading…"))
	case s.err != nil:
		b.WriteString(t.bad.Render("error: " + s.err.Error()))
	case len(s.rows) == 0:
		b.WriteString(t.dim.Render("No scores yet. Be the first!"))
	default:
		b.WriteString(t.dim.Render(fmt.Sprintf("%-4s %-16s %6s %7s", "#", "player", "best", "rounds")) + "\n")
		medals := []string{"🥇", "🥈", "🥉"}
		for i, r := range s.rows {
			rank := fmt.Sprintf("%-3d", i+1)
			if i < len(medals) {
				rank = medals[i] + " "
			}
			line := fmt.Sprintf("%s  %-16s %6d %7d", rank, r.Nick, r.Best, r.Played)
			if r.Nick == s.sess.Nick && !s.sess.Guest {
				line = t.selected.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}
	return t.box.Width(min(52, width-2)).Render(strings.TrimRight(b.String(), "\n"))
}

func (s *leaderboardScreen) Help() string { return "←/→ switch game · r refresh · esc menu" }
