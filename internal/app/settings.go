package app

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"sshhub/internal/store"
)

const (
	settingTheme = iota
	settingNick
	settingClear
	settingBack
	settingCount
)

type settingsMode int

const (
	settingsList settingsMode = iota
	settingsRename
	settingsConfirmClear
)

type (
	themeSavedMsg struct{ err error }
	renamedMsg    struct {
		nick string
		err  error
	}
	scoresClearedMsg struct {
		rounds int64
		err    error
	}
)

type settingsScreen struct {
	deps   *Deps
	sess   *Session
	cursor int
	mode   settingsMode
	input  textinput.Model
	note   string
	noteOK bool
}

func newSettings(deps *Deps, sess *Session) *settingsScreen {
	in := textinput.New()
	in.CharLimit = 16
	in.SetWidth(20)
	return &settingsScreen{deps: deps, sess: sess, input: in}
}

func (s *settingsScreen) Init() tea.Cmd { return nil }

// registered reports whether the session has a saved profile.
func (s *settingsScreen) registered() bool {
	return !s.sess.Guest && s.sess.Fingerprint != "" && s.sess.Nick != ""
}

func (s *settingsScreen) say(ok bool, format string, args ...any) {
	s.note, s.noteOK = fmt.Sprintf(format, args...), ok
}

func (s *settingsScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case themeSavedMsg:
		if msg.err != nil {
			s.say(false, "Couldn't save the theme: %v", msg.err)
		}
		return nil
	case renamedMsg:
		switch {
		case errors.Is(msg.err, store.ErrNickTaken):
			s.say(false, "%q is taken, try another.", msg.nick)
		case msg.err != nil:
			s.say(false, "Couldn't change nickname: %v", msg.err)
		default:
			s.sess.Nick = msg.nick
			s.deps.Hub.SetNick(s.sess.HubID, msg.nick)
			s.mode = settingsList
			s.say(true, "You're now %s, everywhere.", msg.nick)
		}
		return nil
	case scoresClearedMsg:
		s.mode = settingsList
		if msg.err != nil {
			s.say(false, "Couldn't clear scores: %v", msg.err)
		} else {
			s.say(true, "Done. Removed %d rounds from every leaderboard.", msg.rounds)
		}
		return nil
	case tea.KeyPressMsg:
		switch s.mode {
		case settingsRename:
			return s.updateRename(msg)
		case settingsConfirmClear:
			return s.updateConfirm(msg)
		}
		return s.updateList(msg)
	}
	if s.mode == settingsRename {
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return cmd
	}
	return nil
}

func (s *settingsScreen) updateList(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc", "q":
		return back
	case "up", "k":
		s.cursor = (s.cursor - 1 + settingCount) % settingCount
	case "down", "j", "tab":
		s.cursor = (s.cursor + 1) % settingCount
	case "left", "h":
		if s.cursor == settingTheme {
			return s.cycleTheme(-1)
		}
	case "right", "l":
		if s.cursor == settingTheme {
			return s.cycleTheme(1)
		}
	case "enter", "space":
		s.note = ""
		switch s.cursor {
		case settingTheme:
			return s.cycleTheme(1)
		case settingNick:
			if !s.registered() {
				s.say(false, "Guests are named after their SSH username. Connect with a key to pick a nickname.")
				return nil
			}
			s.mode = settingsRename
			s.input.SetValue(s.sess.Nick)
			s.input.CursorEnd()
			return s.input.Focus()
		case settingClear:
			if !s.registered() {
				s.say(false, "Guests have no saved scores.")
				return nil
			}
			s.mode = settingsConfirmClear
		case settingBack:
			return back
		}
	}
	return nil
}

func (s *settingsScreen) cycleTheme(step int) tea.Cmd {
	i := (themeIndex(s.sess.Theme) + step + len(Themes)) % len(Themes)
	s.sess.Theme = Themes[i].Name
	if !s.registered() {
		return nil // guests keep it for this session only
	}
	st, fp, name := s.deps.Store, s.sess.Fingerprint, s.sess.Theme
	return func() tea.Msg { return themeSavedMsg{st.SetTheme(fp, name)} }
}

func (s *settingsScreen) updateRename(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		s.mode = settingsList
		s.input.Blur()
		s.note = ""
		return nil
	case "enter":
		nick := strings.TrimSpace(s.input.Value())
		if !validNick.MatchString(nick) {
			s.say(false, "2-16 characters: letters, digits, _ or -")
			return nil
		}
		st, fp := s.deps.Store, s.sess.Fingerprint
		return func() tea.Msg { return renamedMsg{nick, st.Rename(fp, nick)} }
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(key)
	return cmd
}

func (s *settingsScreen) updateConfirm(key tea.KeyPressMsg) tea.Cmd {
	if key.String() != "y" {
		s.mode = settingsList
		s.say(true, "Phew. Your scores are safe.")
		return nil
	}
	st, fp := s.deps.Store, s.sess.Fingerprint
	return func() tea.Msg {
		n, err := st.ClearScores(fp)
		return scoresClearedMsg{n, err}
	}
}

func (s *settingsScreen) View(width, height int) string {
	t := theme(s.sess)
	w := min(64, width-2)
	var b strings.Builder
	b.WriteString(t.heading.Render("Settings") + "\n\n")

	row := func(i int, label, value string) {
		if i == s.cursor && s.mode == settingsList {
			b.WriteString(t.selected.Render("┃ "+label) + value + "\n")
		} else {
			b.WriteString("  " + label + value + "\n")
		}
	}

	th := t.theme
	value := "  " + t.selected.Render("‹ "+th.Name+" ›") + "  " + swatches(th)
	row(settingTheme, "Theme", value)
	b.WriteString("    " + t.dim.Render(th.Desc) + "\n\n")

	nickValue := ""
	if s.sess.Nick != "" {
		nickValue = "  " + t.dim.Render("("+s.sess.Nick+")")
	}
	row(settingNick, "Change nickname", nickValue)
	if s.mode == settingsRename {
		b.WriteString("    " + s.input.View() + "\n")
	}
	b.WriteString("\n")

	row(settingClear, "Clear my scores", "")
	if s.mode == settingsConfirmClear {
		b.WriteString("\n" + t.bad.Render("    Delete all your scores in every game?") + "\n" +
			"    This can't be undone.\n" +
			"    Press " + t.bad.Render("y") + " to delete, any other key to cancel.\n")
	}
	b.WriteString("\n")
	row(settingBack, "Back", "")

	if s.note != "" {
		style := t.bad
		if s.noteOK {
			style = t.good
		}
		b.WriteString("\n" + lipgloss.NewStyle().Width(w-8).Render(style.Render(s.note)))
	}
	return t.box.Width(w).Render(strings.TrimRight(b.String(), "\n"))
}

// swatches shows a theme's main colors as small blocks.
func swatches(th Theme) string {
	var out string
	for _, hex := range []string{th.Primary, th.Heading, th.Highlight} {
		out += lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("██")
	}
	return out
}

func (s *settingsScreen) Help() string {
	switch s.mode {
	case settingsRename:
		return "enter save · esc cancel"
	case settingsConfirmClear:
		return "y delete everything · any other key cancel"
	}
	return "↑/↓ move · ←/→ change theme · enter select · esc menu"
}
