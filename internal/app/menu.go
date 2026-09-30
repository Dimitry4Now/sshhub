package app

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type menuItem struct {
	title string
	desc  string
	open  func() screen // nil means quit
}

type menuScreen struct {
	items  []menuItem
	cursor int
}

func newMenu(deps *Deps, sess *Session) *menuScreen {
	return &menuScreen{items: []menuItem{
		{"Trivia", "10 questions, beat the clock", func() screen { return newTrivia(deps, sess) }},
		{"Meme gallery", "curated terminal art", func() screen { return newMemes(deps) }},
		{"Leaderboard", "hall of fame", func() screen { return newLeaderboard(deps, sess) }},
		{"Quit", "see you around", nil},
	}}
}

func (s *menuScreen) Init() tea.Cmd { return nil }

func (s *menuScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch k := key.String(); k {
	case "up", "k":
		s.cursor = (s.cursor - 1 + len(s.items)) % len(s.items)
	case "down", "j", "tab":
		s.cursor = (s.cursor + 1) % len(s.items)
	case "enter", "space", "l", "right":
		return s.choose(s.cursor)
	case "q", "esc":
		return tea.Quit
	default:
		if n, err := strconv.Atoi(k); err == nil && n >= 1 && n <= len(s.items) {
			s.cursor = n - 1
			return s.choose(s.cursor)
		}
	}
	return nil
}

func (s *menuScreen) choose(i int) tea.Cmd {
	if s.items[i].open == nil {
		return tea.Quit
	}
	return switchTo(s.items[i].open())
}

func (s *menuScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Welcome to the hub") + "\n\n")
	for i, it := range s.items {
		line := strconv.Itoa(i+1) + ". " + it.title
		if i == s.cursor {
			b.WriteString(selectedStyle.Render("▸ "+line) + "  " + dimStyle.Render(it.desc))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	return boxStyle.Width(min(52, width-2)).Render(strings.TrimRight(b.String(), "\n"))
}

func (s *menuScreen) Help() string { return "↑/↓ move · enter select · 1-4 jump · q quit" }
