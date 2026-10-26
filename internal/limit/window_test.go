package limit

import (
	"testing"
	"time"

	"charm.land/ssh"
)

// fakeContext is a session context that is only ever asked for Done.
type fakeContext struct {
	ssh.Context
	done chan struct{}
}

func (c fakeContext) Done() <-chan struct{} { return c.done }

// fakeSession is a session with a terminal of a given size.
type fakeSession struct {
	ssh.Session
	ctx     ssh.Context
	pty     ssh.Pty
	changes chan ssh.Window
}

func (s *fakeSession) Context() ssh.Context { return s.ctx }
func (s *fakeSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return s.pty, s.changes, true
}

func TestWindowIsCapped(t *testing.T) {
	ctx := fakeContext{done: make(chan struct{})}
	defer close(ctx.done)
	in := &fakeSession{ctx: ctx, pty: ssh.Pty{Window: ssh.Window{Width: 65535, Height: 65535}}, changes: make(chan ssh.Window, 1)}

	got := make(chan ssh.Session, 1)
	Window(400, 150)(func(s ssh.Session) { got <- s })(in)
	s := <-got

	pty, changes, _ := s.Pty()
	if pty.Window.Width != 400 || pty.Window.Height != 150 {
		t.Fatalf("initial window %+v", pty.Window)
	}
	for _, w := range []ssh.Window{{Width: 3000, Height: 40}, {Width: 120, Height: 40}} {
		in.changes <- w
		select {
		case c := <-changes:
			if want := (ssh.Window{Width: min(w.Width, 400), Height: 40}); c != want {
				t.Fatalf("resize to %+v came through as %+v", w, c)
			}
		case <-time.After(time.Second):
			t.Fatal("resize not forwarded")
		}
	}
}
