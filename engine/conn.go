// Package engine groups the TCP connections of one SPOE engine so that an ACK
// may be written on any of them, which is what SPOE 2.0 section 3.2.1's "async"
// capability permits.
package engine

import (
	"fmt"
	"io"
	"net"
	"sync"
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
}

func NewConn(c net.Conn) *Conn {
	return &Conn{conn: c}
}

// Reader exposes the connection for the read loop, which is single-threaded per
// connection and needs no serialisation.
func (c *Conn) Reader() io.Reader {
	return c.conn
}

// WritePayload runs fn with exclusive use of the connection.
func (c *Conn) WritePayload(fn func(io.Writer) error) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.IsClosed() {
		return fmt.Errorf("connection is closed")
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
