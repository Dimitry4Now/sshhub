package app

import (
	"path/filepath"
	"testing"

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
// screen's own result messages are run and fed back; anything else is
// dropped. Text inputs in tests must be still (see still), or their cursor
// blink would make this wait.
func press(s screen, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		switch msg := result(s.Update(k)); msg.(type) {
		case themeSavedMsg, renamedMsg, scoresClearedMsg:
			s.Update(msg)
		}
	}
}

func newTestSettings(t *testing.T, guest bool) (*settingsScreen, *store.Store, *hub.Hub) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := hub.New(nil)
	sess := &Session{Nick: "ada", Guest: guest}
	if !guest {
		sess.Fingerprint = "fp-ada"
		if err := st.Register(sess.Fingerprint, sess.Nick); err != nil {
			t.Fatal(err)
		}
	}
	sess.HubID = h.Connect(hub.Client{Nick: sess.Nick, Guest: guest})
	s := newSettings(&Deps{Store: st, Hub: h}, sess)
	still(&s.input)
	return s, st, h
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

// Tiny or zero-sized terminals must not crash any screen.
func TestScreensSurviveTinyTerminals(t *testing.T) {
	s, st, h := newTestSettings(t, false)
	deps := &Deps{Store: st, Hub: h}
	for _, sc := range []screen{newMenu(deps, s.sess), newChat(deps, s.sess), newSnake(deps, s.sess),
		newTrivia(deps, s.sess), newLeaderboard(deps, s.sess), newMemes(deps, s.sess), s} {
		for _, w := range []int{0, 1, 2, 5} {
			sc.View(w, w)
		}
	}
}
