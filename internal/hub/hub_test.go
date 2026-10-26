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
	a := h.Connect("", "alice", false, nil)
	b := h.Connect("", "bob", true, nil)
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
	first := h.Connect("key:abc", "alice", false, func(r KickReason) {
		if r != KickDuplicate {
			t.Errorf("reason = %v", r)
		}
		close(kicked)
	})
	_, sub := h.Join(first)
	<-sub

	second := h.Connect("key:abc", "alice", false, nil)
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
	third := h.Connect("key:abc", "alice", false, nil)
	if h.Count() != 1 || third == second {
		t.Fatalf("count = %d", h.Count())
	}

	// Empty identity allows duplicates.
	h.Connect("", "dev", false, nil)
	h.Connect("", "dev", false, nil)
	if h.Count() != 3 {
		t.Fatalf("count = %d, want 3", h.Count())
	}
}

func TestHistoryAndArchive(t *testing.T) {
	old := []Message{{Nick: "ada", Text: "before the restart"}}
	h := New(old)
	a := h.Connect("", "alice", false, nil)

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
	a := h.Connect("key:a", "alice", false, kick)
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
	h.Connect("key:b", "bob", false, kick)
	if r := <-kicked; r != KickShutdown {
		t.Fatalf("late session reason = %v", r)
	}
	h.Shutdown() // twice is fine
}
