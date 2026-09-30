package hub

import "testing"

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
	h := New()
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
	h := New()
	kicked := make(chan struct{})
	first := h.Connect("key:abc", "alice", false, func() { close(kicked) })
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
