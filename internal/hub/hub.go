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
	historySize = 100
	maxMsgLen   = 280
	subBuffer   = 64
)

// Message is one chat line. System messages announce joins and leaves.
type Message struct {
	Time   time.Time
	Nick   string
	Text   string
	System bool
}

type member struct {
	nick     string
	guest    bool
	identity string
	kick     func()
	sub      chan Message // non-nil while the member is in the chat lobby
	lastTx   time.Time
}

// Hub is shared by every SSH session.
type Hub struct {
	mu         sync.Mutex
	nextID     int
	members    map[int]*member
	identities map[string]int // identity -> session id, for one session per person
	history    []Message
}

// New creates an empty hub.
func New() *Hub { return &Hub{members: map[int]*member{}, identities: map[string]int{}} }

// Connect registers a session and returns its id.
//
// identity names the person behind the session (e.g. their key fingerprint).
// If another session with the same identity exists, it is removed from the hub
// and its kick function is called so it can close. An empty identity allows
// any number of sessions.
func (h *Hub) Connect(identity, nick string, guest bool, kick func()) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.identities[identity]; ok && identity != "" {
		if m := h.members[old]; m != nil && m.kick != nil {
			go m.kick()
		}
		h.removeLocked(old)
	}
	h.nextID++
	h.members[h.nextID] = &member{nick: nick, guest: guest, identity: identity, kick: kick}
	if identity != "" {
		h.identities[identity] = h.nextID
	}
	return h.nextID
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
	if h.identities[m.identity] == id {
		delete(h.identities, m.identity)
	}
	delete(h.members, id)
}

// SetNick updates a session's nickname (after first-time registration).
func (h *Hub) SetNick(id int, nick string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.members[id]; ok {
		m.nick = nick
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
	if m.nick == "" {
		return "newcomer"
	}
	if m.guest {
		return m.nick + " (guest)"
	}
	return m.nick
}
