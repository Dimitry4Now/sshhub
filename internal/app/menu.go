package app

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
		{"Snake", "the classic, now over SSH", func() screen { return newSnake(deps, sess) }},
		{"Chat lobby", "talk to whoever's around", func() screen { return newChat(deps, sess) }},
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

// logo is "SSH HUB" in the ANSI Shadow figlet font.
var logo = []string{
	"███████╗███████╗██╗  ██╗    ██╗  ██╗██╗   ██╗██████╗ ",
	"██╔════╝██╔════╝██║  ██║    ██║  ██║██║   ██║██╔══██╗",
	"███████╗███████╗███████║    ███████║██║   ██║██████╔╝",
	"╚════██║╚════██║██╔══██║    ██╔══██║██║   ██║██╔══██╗",
	"███████║███████║██║  ██║    ██║  ██║╚██████╔╝██████╔╝",
	"╚══════╝╚══════╝╚═╝  ╚═╝    ╚═╝  ╚═╝ ╚═════╝ ╚═════╝ ",
}

// logoGradient fades from racing green to lime, one color per logo row.
var logoGradient = []string{"#00665E", "#00857C", "#00A19B", "#6FBF4A", "#A5CE1F", "#CEDC00"}

func renderLogo() string {
	rows := make([]string, len(logo))
	for i, row := range logo {
		rows[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(logoGradient[i])).Render(row)
	}
	return strings.Join(rows, "\n")
}

func (s *menuScreen) View(width, height int) string {
	var b strings.Builder
	for i, it := range s.items {
		num := " " + strconv.Itoa(i+1) + " "
		if i == s.cursor {
			b.WriteString(selectedStyle.Render("┃ "+num+" "+it.title) + "\n")
			b.WriteString(selectedStyle.Render("┃ ") + "     " + it.desc + "\n")
		} else {
			b.WriteString("  " + dimStyle.Render(num) + " " + it.title + "\n")
			b.WriteString("       " + dimStyle.Render(it.desc) + "\n")
		}
		if i < len(s.items)-1 {
			b.WriteString("\n")
		}
	}
	menu := boxStyle.Padding(1, 4).Width(min(60, width-2)).Render(strings.TrimRight(b.String(), "\n"))

	// Show the big logo only when it fits above the menu.
	if width >= lipgloss.Width(logo[0])+2 && height >= lipgloss.Height(menu)+len(logo)+2 {
		return lipgloss.JoinVertical(lipgloss.Center, renderLogo(), "", menu)
	}
	return menu
}

func (s *menuScreen) Help() string {
	return "↑/↓ move · enter select · 1-" + strconv.Itoa(len(s.items)) + " jump · q quit"
}
