// Package app implements the per-session terminal UI of the hub.
package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"sshhub/internal/hub"
	"sshhub/internal/store"
)

// Session describes who is connected.
type Session struct {
	Fingerprint string // SSH key fingerprint; empty for guests
	Nick        string // empty until a key user picks one
	Guest       bool   // logged in without a key; scores are not saved
	HubID       int    // presence id in the shared hub
	Theme       string // name of the chosen theme; empty means the default
}

// Deps are shared by every session.
type Deps struct {
	Store     *store.Store
	Hub       *hub.Hub
	Questions []Question
	Memes     []Meme
	Version   string // build version, shown under the menu
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

// KickedMsg tells a session to close, because the same person connected again
// elsewhere, an admin said so, or the server is shutting down.
type KickedMsg struct{ Reason hub.KickReason }

// NoticeMsg carries an admin announcement, shown under the header for a while.
type NoticeMsg struct{ Text string }

const noticeFor = 15 * time.Second

// A session nobody types in for idleFor is closed, so idle visitors can't
// hold connection slots for hours. The SSH server's own idle timeout can't
// do it: the header clock redraws every second, and output counts as
// activity there. The last idleWarn of it shows a countdown.
const (
	idleFor  = 15 * time.Minute
	idleWarn = time.Minute
)

// farewell is shown while a session closes.
type farewell struct{ title, body string }

func kickFarewell(r hub.KickReason) farewell {
	switch r {
	case hub.KickShutdown:
		return farewell{"The hub is restarting", "Your scores are saved. Reconnect in a few seconds."}
	case hub.KickAdmin:
		return farewell{"An admin closed this session", "Your scores are saved."}
	}
	return farewell{"You connected from somewhere else", "Only one session per person. This one is closing."}
}

// Model is the root Bubble Tea model for one SSH session.
type Model struct {
	deps        *Deps
	sess        *Session
	width       int
	height      int
	now         time.Time
	menu        *menuScreen
	screen      screen
	closing     *farewell
	notice      string
	noticeUntil time.Time
	lastInput   time.Time
}

// New builds the root model for a session.
func New(deps *Deps, sess *Session) *Model {
	now := time.Now()
	m := &Model{deps: deps, sess: sess, now: now, lastInput: now, width: 80, height: 24}
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
		// Clients without a real terminal report 0x0; keep the defaults then.
		if msg.Width > 0 && msg.Height > 0 {
			m.width, m.height = msg.Width, msg.Height
		}
		return m, nil
	case NoticeMsg:
		m.notice, m.noticeUntil = msg.Text, time.Now().Add(noticeFor)
		return m, nil
	case KickedMsg:
		return m, m.close(kickFarewell(msg.Reason))
	}
	if m.closing != nil {
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return m, tea.Quit
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.lastInput = m.now
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tea.PasteMsg:
		m.lastInput = m.now
	case tickMsg:
		m.now = time.Time(msg)
		if m.notice != "" && m.now.After(m.noticeUntil) {
			m.notice = ""
		}
		if m.now.Sub(m.lastInput) >= idleFor {
			return m, m.close(farewell{"Closed for inactivity", "No keys pressed for 15 minutes. Reconnect any time."})
		}
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

// close shows f and ends the session a few seconds later, or at a key press.
func (m *Model) close(f farewell) tea.Cmd {
	m.closing = &f
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return tea.QuitMsg{} })
}

func (m *Model) View() tea.View {
	t := theme(m.sess)
	if m.closing != nil {
		msg := t.box.Render(t.heading.Render(m.closing.title) + "\n\n" + m.closing.body)
		v := tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg))
		v.AltScreen = true
		return v
	}
	header := m.header()
	if m.notice != "" {
		header = lipgloss.JoinVertical(lipgloss.Left, header,
			t.selected.Width(m.width).MaxWidth(m.width).Render(" ! "+m.notice))
	}
	if left := idleFor - m.now.Sub(m.lastInput); left <= idleWarn {
		header = lipgloss.JoinVertical(lipgloss.Left, header,
			t.selected.Width(m.width).MaxWidth(m.width).Render(fmt.Sprintf(
				" ! Still there? Closing in %ds, press any key to stay.", int(max(left, 0).Round(time.Second)/time.Second))))
	}
	footer := t.help.Render(m.screen.Help())
	bodyHeight := max(m.height-lipgloss.Height(header)-lipgloss.Height(footer), 1)
	body := lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center,
		m.screen.View(m.width, bodyHeight))

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
	v.AltScreen = true
	v.WindowTitle = "ssh hub"
	return v
}

func (m *Model) header() string {
	t := theme(m.sess)
	who := m.sess.Nick
	if who == "" {
		who = "new user"
	}
	if m.sess.Guest {
		who += " (guest)"
	}
	left := t.title.Render(" ▓ SSH HUB ")
	mid := t.dim.Render(fmt.Sprintf("%s · %d online", who, m.deps.Hub.Count()))
	right := t.clock.Render(m.now.Format("Mon 02 Jan  15:04:05 MST"))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right)
	if gap < 2 {
		mid = ""
		gap = max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	}
	lgap := gap / 2
	line := left + spaces(lgap) + mid + spaces(gap-lgap) + right
	return t.header.Width(m.width).Render(line)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%*s", n, "")
}
