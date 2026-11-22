package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"sshhub/internal/hub"
	"sshhub/internal/store"
)

func TestNoticeShowsThenExpires(t *testing.T) {
	m := New(&Deps{Hub: hub.New(nil)}, &Session{Nick: "ada", Guest: true})
	m.Update(NoticeMsg{Text: "restart at 22:00"})
	if !strings.Contains(m.View().Content, "restart at 22:00") {
		t.Fatal("notice not shown")
	}
	m.Update(tickMsg(time.Now().Add(noticeFor + time.Second)))
	if strings.Contains(m.View().Content, "restart at 22:00") {
		t.Fatal("notice still shown after it expired")
	}
}

func TestIdleSessionCloses(t *testing.T) {
	m := New(&Deps{Hub: hub.New(nil)}, &Session{Nick: "ada", Guest: true})
	start := time.Now()

	m.Update(tickMsg(start.Add(idleFor - idleWarn - time.Second)))
	if strings.Contains(m.View().Content, "Still there?") || m.closing != nil {
		t.Fatal("warned too early")
	}
	m.Update(tickMsg(start.Add(idleFor - 30*time.Second)))
	if !strings.Contains(m.View().Content, "Still there?") {
		t.Fatal("no warning in the last minute")
	}

	// A key press starts the clock again (from the last tick).
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.Update(tickMsg(start.Add(idleFor)))
	if m.closing != nil || strings.Contains(m.View().Content, "Still there?") {
		t.Fatal("key press did not count as activity")
	}

	_, cmd := m.Update(tickMsg(start.Add(2 * idleFor)))
	if m.closing == nil || cmd == nil || !strings.Contains(m.View().Content, "inactivity") {
		t.Fatal("idle session not closed")
	}
}

// result runs cmd and returns its message (nil for a nil command). Only use
// it on commands that finish: a database call, not a timer or a wait for
// chat messages.
func result(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// still stops a text input's cursor from blinking. A blinking cursor returns
// a timer command on every key press, which result would wait out.
func still(in *textinput.Model) {
	st := in.Styles()
	st.Cursor.Blink = false
	in.SetStyles(st)
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestFirstVisitAsksForNickname(t *testing.T) {
	st := newTestStore(t)
	h := hub.New(nil)
	sess := &Session{Fingerprint: "SHA256:new"}
	sess.HubID = h.Connect(hub.Client{Nick: ""})
	m := New(&Deps{Store: st, Hub: h}, sess)
	nick, ok := m.screen.(*nickScreen)
	if !ok {
		t.Fatalf("first screen is %T", m.screen)
	}
	still(&nick.input)
	nick.Init()

	press(nick, key('x'), key(tea.KeyEnter))
	if !strings.Contains(nick.err, "2-16") {
		t.Fatalf("short nickname: err %q", nick.err)
	}
	st.Register("SHA256:other", "taken")
	press(nick, key(tea.KeyBackspace))
	for _, r := range "taken" {
		press(nick, key(r))
	}
	nick.Update(result(nick.Update(key(tea.KeyEnter))))
	if !strings.Contains(nick.err, "taken") {
		t.Fatalf("taken nickname: err %q", nick.err)
	}

	for range 5 {
		press(nick, key(tea.KeyBackspace))
	}
	for _, r := range "ada" {
		press(nick, key(r))
	}
	if cmd := nick.Update(result(nick.Update(key(tea.KeyEnter)))); result(cmd) != (backMsg{}) {
		t.Fatal("not back to the menu after registering")
	}
	if p, ok, _ := st.Profile("SHA256:new"); !ok || p.Nick != "ada" || sess.Nick != "ada" {
		t.Fatalf("profile %+v ok=%v, session nick %q", p, ok, sess.Nick)
	}
	if online := h.Online(); len(online) != 1 || online[0].Nick != "ada" {
		t.Fatalf("hub still shows %+v", online)
	}
}

func TestMenuOpensScreens(t *testing.T) {
	m := New(&Deps{Hub: hub.New(nil)}, &Session{Nick: "ada", Guest: true})
	m.Update(result(m.screen.Update(key('2'))))
	if _, ok := m.screen.(*snakeScreen); !ok {
		t.Fatalf("2 opened %T", m.screen)
	}
	m.Update(backMsg{})
	if m.screen != m.menu {
		t.Fatal("back didn't return to the menu")
	}
	if cmd := m.screen.Update(key('9')); cmd != nil {
		t.Fatal("9 is not a menu item")
	}
	if _, ok := result(m.screen.Update(key('q'))).(tea.QuitMsg); !ok {
		t.Fatal("q doesn't quit")
	}
}
