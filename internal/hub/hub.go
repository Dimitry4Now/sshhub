// Package hub tracks who is connected and relays the shared chat lobby.
package hub

import (
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	historySize   = 100
	maxMsgLen     = 280
	subBuffer     = 64
	archiveBuffer = 256
)

// KickReason says why the hub asked a session to close.
type KickReason int

const (
	// KickDuplicate: the same person connected again elsewhere.
	KickDuplicate KickReason = iota
	// KickShutdown: the server is going down.
	KickShutdown
	// KickAdmin: an admin closed the session.
	KickAdmin
)

// Message is one chat line. System messages announce joins and leaves.
type Message struct {
	Time   time.Time
	Nick   string
	Text   string
	System bool
}

// Client describes a session joining the hub.
type Client struct {
	// Identity names the person behind the session, e.g. their key
	// fingerprint; see Connect.
	Identity string
	Nick     string
	Guest    bool
	Addr     string            // remote address, shown to admins
	Kick     func(KickReason)  // asks the session to close; may be nil
	Notify   func(text string) // shows an announcement; may be nil
}

type member struct {
	Client
	since  time.Time
	sub    chan Message // non-nil while the member is in the chat lobby
	lastTx time.Time
}

// Hub is shared by every SSH session.
type Hub struct {
	mu         sync.Mutex
	nextID     int
	members    map[int]*member
	identities map[string]int // identity -> session id, for one session per person
	history    []Message
	archive    chan Message
	closed     bool
	drained    chan struct{} // closed once shut down and every session has left
}

// New creates a hub whose chat starts with history (oldest first), such as
// messages saved before a restart.
func New(history []Message) *Hub {
	if len(history) > historySize {
		history = history[len(history)-historySize:]
	}
	return &Hub{
		members:    map[int]*member{},
		identities: map[string]int{},
		history:    append([]Message(nil), history...),
		archive:    make(chan Message, archiveBuffer),
		drained:    make(chan struct{}),
	}
}

// Archive delivers every chat message people post (not join/leave notices),
// so they can be saved. If nobody keeps up, messages are dropped rather than
// stalling the chat. The channel is closed by Shutdown.
func (h *Hub) Archive() <-chan Message { return h.archive }

// Shutdown asks every session to close and stops archiving. Sessions that
// connect afterwards are asked to close straight away.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.archive)
	for _, m := range h.members {
		if m.Kick != nil {
			go m.Kick(KickShutdown)
		}
	}
	h.checkDrainedLocked()
}

// Drained is closed once Shutdown was called and every session has left.
func (h *Hub) Drained() <-chan struct{} { return h.drained }

func (h *Hub) checkDrainedLocked() {
	if !h.closed || len(h.members) > 0 {
		return
	}
	select {
	case <-h.drained:
	default:
		close(h.drained)
	}
}

// Connect registers a session and returns its id.
//
// If another session with the same identity exists, it is removed from the
// hub and asked to close. An empty identity allows any number of sessions.
func (h *Hub) Connect(c Client) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed && c.Kick != nil {
		go c.Kick(KickShutdown)
	}
	if old, ok := h.identities[c.Identity]; ok && c.Identity != "" {
		if m := h.members[old]; m != nil && m.Kick != nil {
			go m.Kick(KickDuplicate)
		}
		h.removeLocked(old)
	}
	h.nextID++
	h.members[h.nextID] = &member{Client: c, since: time.Now()}
	if c.Identity != "" {
		h.identities[c.Identity] = h.nextID
	}
	return h.nextID
}

// Kick asks a session to close because an admin said so, and removes it from
// the hub right away. It reports whether the session existed.
func (h *Hub) Kick(id int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, ok := h.members[id]
	if !ok {
		return false
	}
	if m.Kick != nil {
		go m.Kick(KickAdmin)
	}
	h.removeLocked(id)
	return true
}

// Announce shows text to every session and posts it in the chat. It returns
// how many sessions were notified.
func (h *Hub) Announce(text string) int {
	text = Clean(text)
	if text == "" {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.broadcastLocked(Message{Time: time.Now(), Nick: "admin:", Text: text, System: true})
	n := 0
	for _, m := range h.members {
		if m.Notify != nil {
			go m.Notify(text)
			n++
		}
	}
	return n
}

// Session describes one connected session, for admins.
type Session struct {
	ID       int
	Nick     string // as shown to others
	Identity string
	Addr     string
	InChat   bool
	Since    time.Time
}

// Sessions lists connected sessions, oldest first.
func (h *Hub) Sessions() []Session {
	h.mu.Lock()
	out := make([]Session, 0, len(h.members))
	for id, m := range h.members {
		out = append(out, Session{ID: id, Nick: display(m), Identity: m.Identity, Addr: m.Addr, InChat: m.sub != nil, Since: m.since})
	}
	h.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Disconnect removes a session, leaving the chat if it was in it.
func (h *Hub) Disconnect(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(id)
}

func (h *Hub) removeLocked(id int) {
	m, ok := h.members[id]
	if !ok {
		return
	}
	h.leaveLocked(id)
	if h.identities[m.Identity] == id {
		delete(h.identities, m.Identity)
	}
	delete(h.members, id)
	h.checkDrainedLocked()
}

// SetNick updates a session's nickname (after first-time registration).
func (h *Hub) SetNick(id int, nick string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.members[id]; ok {
		m.Nick = nick
	}
}

// Count returns how many sessions are connected.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.members)
}

// Presence is one entry of the online list.
type Presence struct {
	Nick   string
	InChat bool
}

// Online lists connected sessions, chat members first, then by name.
func (h *Hub) Online() []Presence {
	h.mu.Lock()
	out := make([]Presence, 0, len(h.members))
	for _, m := range h.members {
		out = append(out, Presence{Nick: display(m), InChat: m.sub != nil})
	}
	h.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].InChat != out[j].InChat {
			return out[i].InChat
		}
		return strings.ToLower(out[i].Nick) < strings.ToLower(out[j].Nick)
	})
	return out
}

// Join enters the chat lobby. It returns recent history and a channel of new
// messages, which is closed on Leave or Disconnect.
func (h *Hub) Join(id int) ([]Message, <-chan Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, ok := h.members[id]
	if !ok {
		ch := make(chan Message)
		close(ch)
		return nil, ch
	}
	if m.sub != nil {
		close(m.sub)
	}
	m.sub = make(chan Message, subBuffer)
	history := append([]Message(nil), h.history...)
	h.broadcastLocked(Message{Time: time.Now(), Nick: display(m), Text: "joined the chat", System: true})
	return history, m.sub
}

// Leave exits the chat lobby.
func (h *Hub) Leave(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.leaveLocked(id)
}

func (h *Hub) leaveLocked(id int) {
	m, ok := h.members[id]
	if !ok || m.sub == nil {
		return
	}
	close(m.sub)
	m.sub = nil
	h.broadcastLocked(Message{Time: time.Now(), Nick: display(m), Text: "left the chat", System: true})
}

// Send posts a message from a chat member. It returns false when the text is
// empty after cleaning or the sender is posting too fast.
func (h *Hub) Send(id int, text string) bool {
	text = Clean(text)
	if text == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	m, ok := h.members[id]
	if !ok || m.sub == nil || time.Since(m.lastTx) < 500*time.Millisecond {
		return false
	}
	m.lastTx = time.Now()
	h.broadcastLocked(Message{Time: m.lastTx, Nick: display(m), Text: text})
	return true
}

func (h *Hub) broadcastLocked(msg Message) {
	h.history = append(h.history, msg)
	if len(h.history) > historySize {
		h.history = h.history[len(h.history)-historySize:]
	}
	if !msg.System && !h.closed {
		select {
		case h.archive <- msg:
		default: // storage is stuck; keep the chat going
		}
	}
	for _, m := range h.members {
		if m.sub == nil {
			continue
		}
		select {
		case m.sub <- msg:
		default: // slow reader; it can catch up from history on rejoin
		}
	}
}

// Clean strips control characters (including terminal escape sequences'
// ESC byte), collapses whitespace and caps the length.
func Clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxMsgLen {
		s = string(r[:maxMsgLen])
	}
	return s
}

func display(m *member) string {
	if m.Nick == "" {
		return "newcomer"
	}
	if m.Guest {
		return m.Nick + " (guest)"
	}
	return m.Nick
}
