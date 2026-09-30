package app

import (
	"hash/fnv"
	"image/color"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"sshhub/internal/hub"
)

const sidebarWidth = 24

// chatMsg carries one message from the hub subscription.
type chatMsg struct {
	msg hub.Message
	sub <-chan hub.Message
}

// waitChat blocks on the subscription and delivers the next message.
func waitChat(sub <-chan hub.Message) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-sub
		if !ok {
			return nil
		}
		return chatMsg{msg, sub}
	}
}

type chatScreen struct {
	deps   *Deps
	sess   *Session
	sub    <-chan hub.Message
	lines  []hub.Message
	online []hub.Presence
	input  textinput.Model
	note   string
}

func newChat(deps *Deps, sess *Session) *chatScreen {
	in := textinput.New()
	in.Placeholder = "say something…"
	in.CharLimit = 280
	in.Prompt = "› "
	return &chatScreen{deps: deps, sess: sess, input: in}
}

func (s *chatScreen) Init() tea.Cmd {
	s.lines, s.sub = s.deps.Hub.Join(s.sess.HubID)
	s.online = s.deps.Hub.Online()
	return tea.Batch(s.input.Focus(), waitChat(s.sub))
}

func (s *chatScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case chatMsg:
		if msg.sub != s.sub {
			return nil // left over from an earlier visit
		}
		s.lines = append(s.lines, msg.msg)
		if len(s.lines) > 200 {
			s.lines = s.lines[len(s.lines)-200:]
		}
		s.online = s.deps.Hub.Online()
		return waitChat(s.sub)
	case tickMsg:
		s.online = s.deps.Hub.Online()
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			s.deps.Hub.Leave(s.sess.HubID)
			return back
		case "enter":
			text := s.input.Value()
			if hub.Clean(text) == "" {
				return nil
			}
			if s.deps.Hub.Send(s.sess.HubID, text) {
				s.input.Reset()
				s.note = ""
			} else {
				s.note = "slow down a little"
			}
			return nil
		}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return cmd
}

func (s *chatScreen) View(width, height int) string {
	sideW := sidebarWidth
	if width < 60 {
		sideW = 0
	}
	mainW := width - sideW - 2
	s.input.SetWidth(max(mainW-4, 10))

	// Input area: rule, input line, optional note.
	bottom := dimStyle.Render(strings.Repeat("─", mainW)) + "\n" + s.input.View()
	if s.note != "" {
		bottom += "  " + badStyle.Render(s.note)
	}
	logH := max(height-lipgloss.Height(bottom), 1)

	// Render messages newest-last and keep only what fits.
	var rendered []string
	for _, m := range s.lines {
		rendered = append(rendered, s.renderLine(m, mainW))
	}
	log := strings.Join(rendered, "\n")
	if lines := strings.Split(log, "\n"); len(lines) > logH {
		log = strings.Join(lines[len(lines)-logH:], "\n")
	}
	if log == "" {
		log = dimStyle.Render("It's quiet in here. Say hi!")
	}
	log = lipgloss.NewStyle().Width(mainW).Height(logH).AlignVertical(lipgloss.Bottom).Render(log)

	main := lipgloss.JoinVertical(lipgloss.Left, log, bottom)
	if sideW == 0 {
		return lipgloss.NewStyle().Padding(0, 1).Render(main)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Padding(0, 1).Render(main), s.sidebar(sideW, height))
}

func (s *chatScreen) renderLine(m hub.Message, width int) string {
	ts := dimStyle.Render(m.Time.Format("15:04") + " ")
	if m.System {
		return lipgloss.NewStyle().Width(width).Render(ts + dimStyle.Italic(true).Render("• "+m.Nick+" "+m.Text))
	}
	name := lipgloss.NewStyle().Bold(true).Foreground(nickColor(m.Nick)).Render(m.Nick)
	return lipgloss.NewStyle().Width(width).Render(ts + name + " " + m.Text)
}

func (s *chatScreen) sidebar(width, height int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Online") + dimStyle.Render(" ("+strconv.Itoa(len(s.online))+")") + "\n\n")
	for _, p := range s.online {
		dot := dimStyle.Render("○ ")
		if p.InChat {
			dot = goodStyle.Render("● ")
		}
		name := lipgloss.NewStyle().Foreground(nickColor(p.Nick)).MaxWidth(width - 5).Render(p.Nick)
		b.WriteString(dot + name + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("● in chat ○ browsing"))
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(muted).
		Padding(0, 1).
		Render(b.String())
}

func (s *chatScreen) Help() string { return "enter send · esc leave chat" }

// nickPalette colors nicknames consistently across sessions.
var nickPalette = []string{"#CEDC00", "#00A19B", "#3DDC84", "#5FD7FF", "#FFAF5F", "#D7AFFF", "#FF8787", "#87D7AF"}

func nickColor(nick string) color.Color {
	h := fnv.New32a()
	h.Write([]byte(strings.TrimSuffix(nick, " (guest)")))
	return lipgloss.Color(nickPalette[h.Sum32()%uint32(len(nickPalette))])
}
