package engine

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// sink collects everything written to one end of a pipe. net.Pipe is
// unbuffered and synchronous; a Write blocks until a reader consumes all of
// it; so the reader must drain in a loop. Reading a fixed 64 bytes once would
// deadlock any test writing more than that.
type sink struct {
	mu    sync.Mutex
	bytes []byte

	once  sync.Once
	first chan struct{}
}

func newSink() *sink {
	return &sink{first: make(chan struct{})}
}

func (s *sink) add(b []byte) {
	s.mu.Lock()
	s.bytes = append(s.bytes, b...)
	s.mu.Unlock()

	s.once.Do(func() { close(s.first) })
}

// received blocks until something arrives and returns it.
func (s *sink) received(t *testing.T) []byte {
	t.Helper()

	select {
	case <-s.first:
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was written to this connection")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]byte(nil), s.bytes...)
}

// gotNothing reports whether the connection stayed silent. The grace period
// covers the gap between Write returning and the reader recording the bytes.
func (s *sink) gotNothing() bool {
	select {
	case <-s.first:
		return false
	case <-time.After(100 * time.Millisecond):
		return true
	}
}

func pipeConn(t *testing.T, maxFrameSize uint32) (*Conn, *sink) {
	t.Helper()

	client, server := net.Pipe()
	t.Cleanup(func() { client.Close() })

	c := NewConn(server)
	c.SetMaxFrameSize(maxFrameSize)

	s := newSink()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := client.Read(buf)
			if n > 0 {
				s.add(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	return c, s
}

func TestEngine_Write_prefersThePreferredConnection(t *testing.T) {
	e := newEngine("engine-1")

	preferred, preferredSink := pipeConn(t, 16384)
	other, otherSink := pipeConn(t, 16384)

	e.Add(preferred)
	e.Add(other)

	want := []byte{0x00, 0x00, 0x00, 0x01, 0x65}

	if err := e.Write(preferred, want); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := preferredSink.received(t); !bytes.Equal(got, want) {
		t.Fatalf("preferred connection got %v, want %v", got, want)
	}

	if !otherSink.gotNothing() {
		t.Fatal("the frame went to a sibling when the preferred connection was live")
	}
}

// The point of async: the originating connection is gone, so a sibling carries
// the ACK instead of it being lost.
func TestEngine_Write_failsOverToALiveSibling(t *testing.T) {
	e := newEngine("engine-1")

	dead, _ := pipeConn(t, 16384)
	alive, aliveSink := pipeConn(t, 16384)

	e.Add(dead)
	e.Add(alive)

	if err := dead.Close(); err != nil {
		t.Fatalf("closing the preferred connection: %v", err)
	}

	want := []byte{0x00, 0x00, 0x00, 0x01, 0x65}

	if err := e.Write(dead, want); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := aliveSink.received(t); !bytes.Equal(got, want) {
		t.Fatalf("the sibling got %v, want %v", got, want)
	}
}

// max-frame-size is negotiated per connection, so a sibling that agreed a
// smaller ceiling cannot carry the frame.
func TestEngine_Write_skipsAConnectionWhoseCeilingIsTooSmall(t *testing.T) {
	e := newEngine("engine-1")

	dead, _ := pipeConn(t, 16384)
	tooSmall, tooSmallSink := pipeConn(t, 256)
	roomy, roomySink := pipeConn(t, 16384)

	e.Add(dead)
	e.Add(tooSmall)
	e.Add(roomy)

	if err := dead.Close(); err != nil {
		t.Fatalf("closing the preferred connection: %v", err)
	}

	// FRAME-LENGTH excludes its own 4 prefix bytes, so this is a 300-byte frame
	//; above the 256 tooSmall negotiated, below the 16384 roomy did.
	big := make([]byte, 304)

	if err := e.Write(dead, big); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := roomySink.received(t); len(got) != len(big) {
		t.Fatalf("the connection with room got %d bytes, want %d", len(got), len(big))
	}

	if !tooSmallSink.gotNothing() {
		t.Fatal("a frame went to a connection whose negotiated ceiling is smaller than it")
	}
}

func TestEngine_Write_reportsWhenNoConnectionCanCarryIt(t *testing.T) {
	e := newEngine("engine-1")

	only, _ := pipeConn(t, 16384)
	e.Add(only)

	if err := only.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	err := e.Write(only, []byte{0x00, 0x00, 0x00, 0x01, 0x65})
	if !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected ErrNoConnection, got %v", err)
	}
}

// A connection whose write fails is closed before any retry, so a partial frame
// only ever lands on a socket being torn down.
func TestEngine_Write_closesAConnectionThatFailsToWrite(t *testing.T) {
	e := newEngine("engine-1")

	client, server := net.Pipe()
	broken := NewConn(server)
	broken.SetMaxFrameSize(16384)

	// Closing the far end makes the write fail without marking broken closed,
	// which is what distinguishes this from the failover test above.
	if err := client.Close(); err != nil {
		t.Fatalf("closing the peer: %v", err)
	}

	alive, aliveSink := pipeConn(t, 16384)

	e.Add(broken)
	e.Add(alive)

	if err := e.Write(broken, []byte{0x00, 0x00, 0x00, 0x01, 0x65}); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !broken.IsClosed() {
		t.Fatal("a connection whose write failed was not closed")
	}

	if got := aliveSink.received(t); len(got) == 0 {
		t.Fatal("the sibling received nothing after the preferred connection failed")
	}
}

func TestEngine_Remove_reportsWhenTheLastConnectionLeaves(t *testing.T) {
	e := newEngine("engine-1")

	first, _ := pipeConn(t, 16384)
	second, _ := pipeConn(t, 16384)

	e.Add(first)
	e.Add(second)

	if empty := e.Remove(first); empty {
		t.Fatal("the engine reported itself empty while a connection remained")
	}

	if empty := e.Remove(second); !empty {
		t.Fatal("the engine did not report itself empty after the last connection left")
	}
}

func TestEngine_ID(t *testing.T) {
	if got := newEngine("engine-1").ID(); got != "engine-1" {
		t.Fatalf("expected \"engine-1\", got %q", got)
	}
}
