package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"sshhub/internal/hub"
)

func TestGuestsDontShareMemberColors(t *testing.T) {
	if nickColor("alice (guest)") != guestColor || nickColor("bob (guest)") != guestColor {
		t.Fatal("guests not in the guest color")
	}
	for _, p := range []string{"alice", "bob", "carol", "dave"} {
		if nickColor(p) == guestColor {
			t.Fatalf("%s has the guest color", p)
		}
		if nickColor(p) != nickColor(p) {
			t.Fatalf("%s's color changes", p)
		}
	}
}

func TestChatFlow(t *testing.T) {
	h := hub.New([]hub.Message{{Nick: "ada", Text: "from before"}})
	sess := &Session{Nick: "ada", Fingerprint: "SHA256:ada"}
	sess.HubID = h.Connect(hub.Client{Nick: "ada", Addr: "203.0.113.1:1"})
	bob := h.Connect(hub.Client{Nick: "bob", Addr: "198.51.100.2:2"})
	_, bobSub := h.Join(bob)
	<-bobSub

	s := newChat(&Deps{Hub: h}, sess)
	still(&s.input)
	s.Init()
	if msg := <-bobSub; msg.Text != "joined the chat" || msg.Nick != "ada" {
		t.Fatalf("bob got %+v", msg)
	}
	if view := s.View(100, 20); !strings.Contains(view, "from before") || !strings.Contains(view, "Online") {
		t.Fatalf("history or online list missing:\n%s", view)
	}
	if strings.Contains(s.View(50, 20), "Online") {
		t.Fatal("online list shown in a narrow terminal")
	}

	press(s, key('h'), key('i'))
	s.Update(key(tea.KeyEnter))
	if msg := <-bobSub; msg.Text != "hi" || s.input.Value() != "" {
		t.Fatalf("bob got %+v, input %q", msg, s.input.Value())
	}
	press(s, key('x'))
	s.Update(key(tea.KeyEnter))
	if !strings.Contains(s.note, "slow down") || s.input.Value() != "x" {
		t.Fatalf("fast second message: note %q input %q", s.note, s.input.Value())
	}

	// Messages from the hub show up; leftovers from an earlier visit don't.
	if cmd := s.Update(chatMsg{hub.Message{Nick: "bob", Text: "hello ada"}, s.sub}); cmd == nil {
		t.Fatal("stopped listening after a message")
	}
	s.Update(chatMsg{hub.Message{Nick: "ghost", Text: "old"}, make(chan hub.Message)})
	if view := s.View(100, 20); !strings.Contains(view, "hello ada") || strings.Contains(view, "old") {
		t.Fatalf("view:\n%s", view)
	}

	if _, ok := result(s.Update(key(tea.KeyEscape))).(backMsg); !ok {
		t.Fatal("esc doesn't go back")
	}
	if msg := <-bobSub; msg.Text != "left the chat" {
		t.Fatalf("bob got %+v", msg)
	}
	for _, p := range h.Online() {
		if p.Nick == "ada" && p.InChat {
			t.Fatal("ada still in the chat after leaving")
		}
	}
}
