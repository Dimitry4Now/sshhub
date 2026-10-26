package hub

import (
	"testing"
	"time"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"hello":                  "hello",
		"  spaced \t out \n ":    "spaced out",
		"\x1b[31mred\x1b[0m":     "[31mred [0m",
		"bell\x07 and \u009bcsi": "bell and csi",
		"":                       "",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChatFlow(t *testing.T) {
	h := New(nil)
	a := h.Connect(Client{Nick: "alice"})
	b := h.Connect(Client{Nick: "bob", Guest: true})
	if h.Count() != 2 {
		t.Fatalf("count = %d", h.Count())
	}

	_, chA := h.Join(a)
	<-chA // alice's own join notice
	_, chB := h.Join(b)
	if msg := <-chA; !msg.System || msg.Nick != "bob (guest)" {
		t.Fatalf("unexpected join message %+v", msg)
	}
	<-chB

	if !h.Send(a, "hi") {
		t.Fatal("send failed")
	}
	if h.Send(a, "again") {
		t.Fatal("rate limit not applied")
	}
	for _, ch := range []<-chan Message{chA, chB} {
		if msg := <-ch; msg.Text != "hi" || msg.Nick != "alice" {
			t.Fatalf("got %+v", msg)
		}
	}

	h.Disconnect(b)
	if _, ok := <-chB; ok {
		t.Fatal("channel not closed on disconnect")
	}
	if msg := <-chA; msg.Text != "left the chat" {
		t.Fatalf("got %+v", msg)
	}
	if h.Count() != 1 {
		t.Fatalf("count = %d", h.Count())
	}
}

func TestOneSessionPerIdentity(t *testing.T) {
	h := New(nil)
	kicked := make(chan struct{})
	first := h.Connect(Client{Identity: "key:abc", Nick: "alice", Kick: func(r KickReason) {
		if r != KickDuplicate {
			t.Errorf("reason = %v", r)
		}
		close(kicked)
	}})
	_, sub := h.Join(first)
	<-sub

	second := h.Connect(Client{Identity: "key:abc", Nick: "alice"})
	<-kicked
	if _, ok := <-sub; ok {
		t.Fatal("old chat subscription not closed")
	}
	if h.Count() != 1 {
		t.Fatalf("count = %d, want 1", h.Count())
	}

	// The old session's late disconnect must not drop the new one.
	h.Disconnect(first)
	if h.Count() != 1 {
		t.Fatalf("count = %d after stale disconnect", h.Count())
	}
	third := h.Connect(Client{Identity: "key:abc", Nick: "alice"})
	if h.Count() != 1 || third == second {
		t.Fatalf("count = %d", h.Count())
	}

	// Empty identity allows duplicates.
	h.Connect(Client{Nick: "dev"})
	h.Connect(Client{Nick: "dev"})
	if h.Count() != 3 {
		t.Fatalf("count = %d, want 3", h.Count())
	}
}

func TestHistoryAndArchive(t *testing.T) {
	old := []Message{{Nick: "ada", Text: "before the restart"}}
	h := New(old)
	a := h.Connect(Client{Nick: "alice"})

	history, sub := h.Join(a)
	if len(history) != 1 || history[0].Text != "before the restart" {
		t.Fatalf("history = %+v", history)
	}
	<-sub
	if !h.Send(a, "hello") {
		t.Fatal("send failed")
	}

	// Only the posted message is archived, not the join notice.
	select {
	case msg := <-h.Archive():
		if msg.Text != "hello" || msg.Nick != "alice" {
			t.Fatalf("archived %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing archived")
	}

	h.Shutdown()
	if _, ok := <-h.Archive(); ok {
		t.Fatal("archive not closed by shutdown")
	}
}

func TestShutdownKicksEveryone(t *testing.T) {
	h := New(nil)
	kicked := make(chan KickReason, 2)
	kick := func(r KickReason) { kicked <- r }
	a := h.Connect(Client{Identity: "key:a", Nick: "alice", Kick: kick})
	h.Shutdown()
	if r := <-kicked; r != KickShutdown {
		t.Fatalf("reason = %v", r)
	}
	select {
	case <-h.Drained():
		t.Fatal("drained while alice is still connected")
	default:
	}
	h.Disconnect(a)
	<-h.Drained()

	// A session that slips in during shutdown is told to leave too.
	h.Connect(Client{Identity: "key:b", Nick: "bob", Kick: kick})
	if r := <-kicked; r != KickShutdown {
		t.Fatalf("late session reason = %v", r)
	}
	h.Shutdown() // twice is fine
}

func TestAdminKickAnnounceSessions(t *testing.T) {
	h := New(nil)
	kicked := make(chan KickReason, 1)
	notified := make(chan string, 2)
	notify := func(s string) { notified <- s }
	a := h.Connect(Client{Identity: "key:a", Nick: "alice", Addr: "10.0.0.1:5000", Notify: notify})
	b := h.Connect(Client{Nick: "bob", Guest: true, Kick: func(r KickReason) { kicked <- r }, Notify: notify})

	sessions := h.Sessions()
	if len(sessions) != 2 || sessions[0].ID != a || sessions[0].Addr != "10.0.0.1:5000" || sessions[1].Nick != "bob (guest)" {
		t.Fatalf("sessions = %+v", sessions)
	}

	_, sub := h.Join(a)
	<-sub
	if n := h.Announce("restart in 5 minutes \x1b[2J"); n != 2 {
		t.Fatalf("notified %d sessions", n)
	}
	for range 2 {
		if got := <-notified; got != "restart in 5 minutes [2J" {
			t.Fatalf("notice %q", got)
		}
	}
	if msg := <-sub; !msg.System || msg.Nick != "admin:" {
		t.Fatalf("chat got %+v", msg)
	}
	if h.Announce("  ") != 0 {
		t.Fatal("empty announcement sent")
	}

	if !h.Kick(b) {
		t.Fatal("kick failed")
	}
	if r := <-kicked; r != KickAdmin {
		t.Fatalf("reason = %v", r)
	}
	if h.Count() != 1 || h.Kick(b) {
		t.Fatal("kicked session still there")
	}
}
