package app

import (
	"errors"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"sshhub/internal/store"
)

var validNick = regexp.MustCompile(`^[A-Za-z0-9_-]{2,16}$`)

type nickResultMsg struct {
	nick string
	err  error
}

// nickScreen asks a first-time key user to pick a nickname.
type nickScreen struct {
	deps  *Deps
	sess  *Session
	input textinput.Model
	err   string
}

func newNickScreen(deps *Deps, sess *Session) *nickScreen {
	in := textinput.New()
	in.Placeholder = "nickname"
	in.CharLimit = 16
	in.SetWidth(20)
	return &nickScreen{deps: deps, sess: sess, input: in}
}

func (s *nickScreen) Init() tea.Cmd { return s.input.Focus() }

func (s *nickScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case nickResultMsg:
		switch {
		case errors.Is(msg.err, store.ErrNickTaken):
			s.err = "That nickname is taken, try another."
		case msg.err != nil:
			s.err = "Something went wrong: " + msg.err.Error()
		default:
			s.sess.Nick = msg.nick
			s.deps.Hub.SetNick(s.sess.HubID, msg.nick)
			return back
		}
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			nick := strings.TrimSpace(s.input.Value())
			if !validNick.MatchString(nick) {
				s.err = "2-16 characters: letters, digits, _ or -"
				return nil
			}
			st, fp := s.deps.Store, s.sess.Fingerprint
			return func() tea.Msg { return nickResultMsg{nick, st.Register(fp, nick)} }
		case "esc":
			return tea.Quit
		}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return cmd
}

func (s *nickScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Hey, new face!") + "\n\n")
	b.WriteString("Pick a nickname. It's tied to your SSH key,\nso next time you'll be recognized automatically.\n\n")
	b.WriteString(s.input.View())
	if s.err != "" {
		b.WriteString("\n\n" + badStyle.Render(s.err))
	}
	return boxStyle.Width(min(56, width-2)).Render(b.String())
}

func (s *nickScreen) Help() string { return "enter confirm · esc leave" }
