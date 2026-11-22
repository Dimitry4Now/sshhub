package app

import "testing"

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
