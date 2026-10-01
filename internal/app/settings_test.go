package app

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"sshhub/internal/hub"
	"sshhub/internal/store"
)

func key(code rune) tea.KeyPressMsg {
	k := tea.KeyPressMsg{Code: code}
	if code >= ' ' && code < 0x7f {
		k.Text = string(code)
	}
	return k
}

// press sends keys to the screen. Commands that produce one of the settings
// screen's own result messages are run and fed back; anything else (such as
// the text input's cursor blink, which re-arms itself forever) is dropped.
func press(s screen, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		if cmd := s.Update(k); cmd != nil {
			feed(s, cmd)
		}
	}
}

func feed(s screen, cmd tea.Cmd) {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch msg.(type) {
		case themeSavedMsg, renamedMsg, scoresClearedMsg:
			s.Update(msg)
		}
	case <-time.After(100 * time.Millisecond): // a timer, not a result
	}
}

func newTestSettings(t *testing.T, guest bool) (*settingsScreen, *store.Store, *hub.Hub) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := hub.New()
	sess := &Session{Nick: "ada", Guest: guest}
	if !guest {
		sess.Fingerprint = "fp-ada"
		if err := st.Register(sess.Fingerprint, sess.Nick); err != nil {
			t.Fatal(err)
		}
	}
	sess.HubID = h.Connect("", sess.Nick, guest, nil)
	return newSettings(&Deps{Store: st, Hub: h}, sess), st, h
}

func TestSettingsThemeIsSaved(t *testing.T) {
	s, st, _ := newTestSettings(t, false)
	press(s, key(tea.KeyRight))
	if s.sess.Theme != Themes[1].Name {
		t.Fatalf("theme = %q", s.sess.Theme)
	}
	if p, _, _ := st.Profile("fp-ada"); p.Theme != Themes[1].Name {
		t.Fatalf("saved theme = %q", p.Theme)
	}
}

func TestSettingsRename(t *testing.T) {
	s, st, h := newTestSettings(t, false)
	st.Register("fp-linus", "linus")
	st.AddScore("fp-ada", "trivia", 300)

	s.cursor = settingNick
	press(s, key(tea.KeyEnter))
	for range len("ada") {
		press(s, key(tea.KeyBackspace))
	}
	press(s, key('l'), key('i'), key('n'), key('u'), key('s'), key(tea.KeyEnter))
	if s.sess.Nick != "ada" || s.mode != settingsRename {
		t.Fatalf("took a taken nick: nick=%q mode=%v", s.sess.Nick, s.mode)
	}

	press(s, key(tea.KeyBackspace), key(tea.KeyBackspace), key(tea.KeyBackspace),
		key(tea.KeyBackspace), key(tea.KeyBackspace))
	press(s, key('a'), key('d'), key('a'), key('2'), key(tea.KeyEnter))
	if s.sess.Nick != "ada2" || s.mode != settingsList {
		t.Fatalf("nick=%q mode=%v note=%q", s.sess.Nick, s.mode, s.note)
	}
	if top, _ := st.Top("trivia", 10); len(top) != 1 || top[0].Nick != "ada2" {
		t.Fatalf("leaderboard = %+v", top)
	}
	if online := h.Online(); online[0].Nick != "ada2" {
		t.Fatalf("online = %+v", online)
	}
}

func TestSettingsClearNeedsConfirmation(t *testing.T) {
	s, st, _ := newTestSettings(t, false)
	st.AddScore("fp-ada", "trivia", 300)
	st.AddScore("fp-ada", "snake", 40)

	s.cursor = settingClear
	press(s, key(tea.KeyEnter), key('n'))
	if top, _ := st.Top("trivia", 10); len(top) != 1 {
		t.Fatal("scores cleared without confirmation")
	}

	press(s, key(tea.KeyEnter), key('y'))
	for _, game := range []string{"trivia", "snake"} {
		if top, _ := st.Top(game, 10); len(top) != 0 {
			t.Fatalf("%s scores left: %+v", game, top)
		}
	}
}

func TestSettingsGuestLimits(t *testing.T) {
	s, _, _ := newTestSettings(t, true)
	s.cursor = settingNick
	press(s, key(tea.KeyEnter))
	if s.mode != settingsList {
		t.Fatal("guest could open rename")
	}
	s.cursor = settingClear
	press(s, key(tea.KeyEnter))
	if s.mode != settingsList {
		t.Fatal("guest could open clear scores")
	}
	s.cursor = settingTheme
	press(s, key(tea.KeyRight))
	if s.sess.Theme != Themes[1].Name {
		t.Fatal("guest couldn't change theme for the session")
	}
}
