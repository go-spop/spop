package engine

import (
	"errors"
	"fmt"
	"sync"
)

// ErrNoConnection reports that no member of the engine could carry a frame:
// every one is closed, or its negotiated ceiling is smaller than the frame.
var ErrNoConnection = errors.New("no connection in the engine can carry the frame")

// frameLengthPrefix is the 4-byte FRAME-LENGTH field, which the length it
// declares does not itself count.
const frameLengthPrefix = 4

// Engine is the set of connections belonging to one SPOE engine. Section 3.2.1
// permits an ACK to go out on any connection of the engine that sent the
// NOTIFY, and names "engine-id" as the value connections are grouped by.
type Engine struct {
	id string

	mu    sync.RWMutex
	conns []*Conn
}

func newEngine(id string) *Engine {
	return &Engine{id: id}
}

func (e *Engine) ID() string {
	return e.id
}

func (e *Engine) Add(c *Conn) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.conns = append(e.conns, c)
}

// Remove drops a connection and reports whether THIS call emptied the engine,
// which is the registry's cue to forget it. A connection that was not a member
// reports false however empty the engine is: the registry decrements a counter
// on the strength of this answer, so "already empty" must not be mistaken for
// "just emptied".
func (e *Engine) Remove(c *Conn) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	removed := false

	for i, existing := range e.conns {
		if existing == c {
			e.conns = append(e.conns[:i], e.conns[i+1:]...)
			removed = true
			break
		}
	}

	return removed && len(e.conns) == 0
}

// Write delivers an encoded frame, preferring the connection its NOTIFY arrived
// on and falling back to a live sibling. A connection whose write fails is
// closed before the next candidate is tried, so a partially written frame only
// lands on a socket that is being torn down anyway.
func (e *Engine) Write(preferred *Conn, frame []byte) error {
	if len(frame) < frameLengthPrefix {
		return fmt.Errorf("frame of %d bytes is shorter than its own length prefix", len(frame))
	}

	for _, c := range e.candidates(preferred, uint32(len(frame)-frameLengthPrefix)) {
		if err := c.WriteFrame(frame); err == nil {
			return nil
		}

		// The frame may have gone out in part. Take the connection down rather
		// than leave the peer parsing from the wrong offset.
		_ = c.Close()
	}

	return ErrNoConnection
}

// candidates lists the connections that could carry a frame of this length,
// preferred first. max-frame-size is negotiated per connection, so a sibling
// that agreed a smaller ceiling is not a candidate.
//
// usable is evaluated purely from preferred's own closed/size state, without
// checking whether preferred is still a member of e.conns. That is
// deliberate: a worker that has already left the engine (its teardown calls
// leaveEngine before closing the connection) can still be mid-write on its
// own, still-live socket, and that in-flight ACK must be allowed to use it.
func (e *Engine) candidates(preferred *Conn, length uint32) []*Conn {
	e.mu.RLock()
	defer e.mu.RUnlock()

	usable := func(c *Conn) bool {
		return c != nil && !c.IsClosed() && length <= c.MaxFrameSize()
	}

	out := make([]*Conn, 0, len(e.conns))

	if usable(preferred) {
		out = append(out, preferred)
	}

	for _, c := range e.conns {
		if c != preferred && usable(c) {
			out = append(out, c)
		}
	}

	return out
}
