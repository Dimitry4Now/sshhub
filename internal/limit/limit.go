// Package limit caps how many connections are open at once, in total and per
// remote address, so one visitor can't use up the server.
package limit

import (
	"net"
	"sync"
)

// Conns counts open connections.
type Conns struct {
	maxTotal, maxPerHost int

	mu     sync.Mutex
	total  int
	byHost map[string]int
}

// NewConns allows at most maxTotal connections, and at most maxPerHost from
// one address.
func NewConns(maxTotal, maxPerHost int) *Conns {
	return &Conns{maxTotal: maxTotal, maxPerHost: maxPerHost, byHost: map[string]int{}}
}

// Admit returns conn wrapped so that closing it frees its slot, or nil when a
// limit is reached. The caller closes conn in that case.
func (c *Conns) Admit(conn net.Conn) net.Conn {
	host := hostOf(conn.RemoteAddr())
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.total >= c.maxTotal || c.byHost[host] >= c.maxPerHost {
		return nil
	}
	c.total++
	c.byHost[host]++
	return &countedConn{Conn: conn, release: func() { c.release(host) }}
}

func (c *Conns) release(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total--
	if c.byHost[host]--; c.byHost[host] <= 0 {
		delete(c.byHost, host)
	}
}

// Open returns how many connections are open.
func (c *Conns) Open() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

func hostOf(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

type countedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

// Close may be called more than once; the slot is freed the first time.
func (c *countedConn) Close() error {
	c.once.Do(c.release)
	return c.Conn.Close()
}
