package limit

import (
	"charm.land/ssh"
	"charm.land/wish/v2"
)

// Window caps the terminal size a session reports, in cells. The client
// chooses its size freely, and drawing the hub costs memory that grows with
// width × height: a single session claiming 2000x1000 used half a GiB, so
// without a cap anyone could run the server out of memory. A bigger terminal
// just shows the hub in its top left corner.
func Window(maxWidth, maxHeight int) wish.Middleware {
	clamp := func(w ssh.Window) ssh.Window {
		w.Width, w.Height = min(w.Width, maxWidth), min(w.Height, maxHeight)
		return w
	}
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			pty, changes, ok := s.Pty()
			if !ok {
				next(s)
				return
			}
			pty.Window = clamp(pty.Window)
			clamped := make(chan ssh.Window, 1)
			go func() {
				for {
					select {
					case <-s.Context().Done():
						return
					case w, ok := <-changes:
						if !ok {
							return
						}
						select {
						case clamped <- clamp(w):
						case <-s.Context().Done():
							return
						}
					}
				}
			}()
			next(&boundedSession{Session: s, pty: pty, changes: clamped})
		}
	}
}

// boundedSession reports the capped terminal size.
type boundedSession struct {
	ssh.Session
	pty     ssh.Pty
	changes chan ssh.Window
}

func (s *boundedSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return s.pty, s.changes, true
}
