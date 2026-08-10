// Package engine groups the TCP connections of one SPOE engine so that an ACK
// may be written on any of them, which is what SPOE 2.0 section 3.2.1's "async"
// capability permits.
package engine

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Conn is one connection to a peer. Writes are serialised across whole
// payloads: section 3.2 forbids interleaving the frames of one fragmented
// payload with anything else on the same connection, so the lock spans a
// payload rather than a frame.
type Conn struct {
	conn net.Conn

	writeMu sync.Mutex

	mu           sync.RWMutex
	closed       bool
	maxFrameSize uint32
	writeTimeout time.Duration
}

func NewConn(c net.Conn) *Conn {
	return &Conn{conn: c}
}

// Reader exposes the connection for the read loop, which is single-threaded per
// connection and needs no serialisation.
func (c *Conn) Reader() io.Reader {
	return c.conn
}

// WritePayload runs fn with exclusive use of the connection. A write timeout,
// when set, bounds the whole payload rather than each Write inside it -- the
// callback is one payload, and a peer that stops reading half way through it
// has wedged the connection just as surely as one that never read at all.
func (c *Conn) WritePayload(fn func(io.Writer) error) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.IsClosed() {
		return fmt.Errorf("connection is closed")
	}

	if d := c.WriteTimeout(); d > 0 {
		if err := c.conn.SetWriteDeadline(time.Now().Add(d)); err != nil {
			return err
		}

		// Clear it, or the deadline outlives this payload and expires the next
		// write on this connection.
		defer func() { _ = c.conn.SetWriteDeadline(time.Time{}) }()
	}

	return fn(c.conn)
}

// WriteFrame writes one encoded frame. A short write leaves the peer parsing
// from the wrong offset, so it is an error rather than something to retry.
func (c *Conn) WriteFrame(frame []byte) error {
	return c.WritePayload(func(w io.Writer) error {
		n, err := w.Write(frame)
		if err != nil {
			return err
		}

		if n != len(frame) {
			return io.ErrShortWrite
		}

		return nil
	})
}

// Close is safe to call more than once: a connection can be torn down by the
// read loop and by a NOTIFY goroutine that cannot write its ACK.
//
// Close deliberately does not take writeMu. A write can be blocked in the
// underlying net.Conn indefinitely; closing the socket out from under it is
// what unblocks that write with an error, which is what lets a stuck writer
// be torn down at all. Taking writeMu here would instead make Close wait
// behind that same blocked write, reintroducing the hang this is meant to
// break.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	return c.conn.Close()
}

func (c *Conn) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.closed
}

// SetMaxFrameSize records what this connection negotiated. Called once, at the
// end of the HELLO handshake, before the connection joins an engine.
func (c *Conn) SetMaxFrameSize(size uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.maxFrameSize = size
}

func (c *Conn) MaxFrameSize() uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.maxFrameSize
}

// SetWriteTimeout bounds how long a payload may take to write. Zero disables
// it, matching net.Conn deadline semantics. Called once at the construction
// site before the connection is used, as SetMaxFrameSize is.
func (c *Conn) SetWriteTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writeTimeout = d
}

func (c *Conn) WriteTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.writeTimeout
}

// SetReadDeadline lets the read loop bound its own next read, and is also how
// Agent.Shutdown wakes a read blocked in that loop without closing the
// socket, so an in-flight ACK can still go out. That cross-goroutine call is
// by design -- the drain poke comes from Shutdown, the arm from the read loop
// -- and it is sound because net.Conn's deadline setters are documented as
// safe for concurrent use.
func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}
