package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"sshhub/internal/hub"
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
