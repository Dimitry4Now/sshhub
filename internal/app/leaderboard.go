package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"sshhub/internal/store"
)

type leaderboardMsg struct {
	rows []store.Score
	err  error
}

type leaderboardScreen struct {
	deps    *Deps
	sess    *Session
	rows    []store.Score
	err     error
	loading bool
}

func newLeaderboard(deps *Deps, sess *Session) *leaderboardScreen {
	return &leaderboardScreen{deps: deps, sess: sess}
}

func (s *leaderboardScreen) Init() tea.Cmd {
	s.loading = true
	st := s.deps.Store
	return func() tea.Msg {
		rows, err := st.Top(triviaGame, 10)
		return leaderboardMsg{rows, err}
	}
}

func (s *leaderboardScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case leaderboardMsg:
		s.rows, s.err, s.loading = msg.rows, msg.err, false
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			return back
		case "r":
			return s.Init()
		}
	}
	return nil
}

func (s *leaderboardScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("🏆 Trivia leaderboard") + "\n\n")
	switch {
	case s.loading:
		b.WriteString(dimStyle.Render("loading…"))
	case s.err != nil:
		b.WriteString(badStyle.Render("error: " + s.err.Error()))
	case len(s.rows) == 0:
		b.WriteString(dimStyle.Render("No scores yet. Be the first!"))
	default:
		b.WriteString(dimStyle.Render(fmt.Sprintf("%-4s %-16s %6s %7s", "#", "player", "best", "rounds")) + "\n")
		medals := []string{"🥇", "🥈", "🥉"}
		for i, r := range s.rows {
			rank := fmt.Sprintf("%-3d", i+1)
			if i < len(medals) {
				rank = medals[i] + " "
			}
			line := fmt.Sprintf("%s  %-16s %6d %7d", rank, r.Nick, r.Best, r.Played)
			if r.Nick == s.sess.Nick && !s.sess.Guest {
				line = selectedStyle.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}
	return boxStyle.Width(min(52, width-2)).Render(strings.TrimRight(b.String(), "\n"))
}

func (s *leaderboardScreen) Help() string { return "r refresh · esc menu" }
