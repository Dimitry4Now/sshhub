package limit

import (
	"net"
	"testing"
)

// fakeConn is a connection from a given address.
type fakeConn struct {
	net.Conn
	addr net.Addr
}

func (c fakeConn) RemoteAddr() net.Addr { return c.addr }
func (c fakeConn) Close() error         { return nil }

func from(ip string) net.Conn {
	return fakeConn{addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 50000}}
}

func TestPerHostLimit(t *testing.T) {
	c := NewConns(10, 2)
	a1, a2 := c.Admit(from("10.0.0.1")), c.Admit(from("10.0.0.1"))
	if a1 == nil || a2 == nil {
		t.Fatal("first two connections refused")
	}
	if c.Admit(from("10.0.0.1")) != nil {
		t.Fatal("third connection from the same host admitted")
	}
	if c.Admit(from("10.0.0.2")) == nil {
		t.Fatal("other host refused")
	}

	a1.Close()
	a1.Close() // a second close must not free another slot
	if c.Open() != 2 {
		t.Fatalf("open = %d, want 2", c.Open())
	}
	if c.Admit(from("10.0.0.1")) == nil {
		t.Fatal("slot not freed on close")
	}
	if c.Admit(from("10.0.0.1")) != nil {
		t.Fatal("double close freed two slots")
	}
}

func TestTotalLimit(t *testing.T) {
	c := NewConns(2, 2)
	c.Admit(from("10.0.0.1"))
	last := c.Admit(from("10.0.0.2"))
	if c.Admit(from("10.0.0.3")) != nil {
		t.Fatal("total limit not applied")
	}
	last.Close()
	if c.Admit(from("10.0.0.3")) == nil {
		t.Fatal("slot not freed")
	}
}
