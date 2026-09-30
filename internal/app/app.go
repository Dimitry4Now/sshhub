// Package app implements the per-session terminal UI of the hub.
package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"sshhub/internal/store"
)

// Session describes who is connected.
type Session struct {
	Fingerprint string // SSH key fingerprint; empty for guests
	Nick        string // empty until a key user picks one
	Guest       bool   // logged in without a key; scores are not saved
}

// Deps are shared by every session.
type Deps struct {
	Store     *store.Store
	Online    func() int
	Questions []Question
	Memes     []Meme
}

// screen is one page of the hub (menu, trivia, ...).
type screen interface {
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	View(width, height int) string
	Help() string
}

type (
	tickMsg   time.Time
	switchMsg struct{ to screen }
	backMsg   struct{}
)

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func switchTo(s screen) tea.Cmd { return func() tea.Msg { return switchMsg{to: s} } }

func back() tea.Msg { return backMsg{} }

// Model is the root Bubble Tea model for one SSH session.
type Model struct {
	deps   *Deps
	sess   *Session
	width  int
	height int
	now    time.Time
	menu   *menuScreen
	screen screen
}

// New builds the root model for a session.
func New(deps *Deps, sess *Session) *Model {
	m := &Model{deps: deps, sess: sess, now: time.Now(), width: 80, height: 24}
	m.menu = newMenu(deps, sess)
	m.screen = m.menu
	if !sess.Guest && sess.Nick == "" {
		m.screen = newNickScreen(deps, sess)
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tick(), m.screen.Init())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tickMsg:
		m.now = time.Time(msg)
		return m, tea.Batch(tick(), m.screen.Update(msg))
	case switchMsg:
		m.screen = msg.to
		return m, m.screen.Init()
	case backMsg:
		m.screen = m.menu
		return m, m.menu.Init()
	}
	return m, m.screen.Update(msg)
}

func (m *Model) View() tea.View {
	header := m.header()
	footer := helpStyle.Render(m.screen.Help())
	bodyHeight := max(m.height-lipgloss.Height(header)-lipgloss.Height(footer), 1)
	body := lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center,
		m.screen.View(m.width, bodyHeight))

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
	v.AltScreen = true
	v.WindowTitle = "ssh hub"
	return v
}

func (m *Model) header() string {
	who := m.sess.Nick
	if who == "" {
		who = "new user"
	}
	if m.sess.Guest {
		who += " (guest)"
	}
	left := titleStyle.Render(" ▓ SSH HUB ")
	mid := dimStyle.Render(fmt.Sprintf("%s · %d online", who, m.deps.Online()))
	right := clockStyle.Render(m.now.Format("Mon 02 Jan  15:04:05 MST"))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right)
	if gap < 2 {
		mid = ""
		gap = max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	}
	lgap := gap / 2
	line := left + spaces(lgap) + mid + spaces(gap-lgap) + right
	return headerStyle.Width(m.width).Render(line)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%*s", n, "")
}
