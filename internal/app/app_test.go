package app

import (
	"strings"
	"testing"
	"time"

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
