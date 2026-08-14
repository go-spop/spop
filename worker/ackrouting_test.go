package worker

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/go-spop/spop/action"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/request"
)

// An ACK goes back on the connection its NOTIFY arrived on, and nowhere else.
//
// Two connections announce the same engine-id and both name async in their
// capabilities, which is everything section 3.2.1 asks for before an ACK may be
// carried by a sibling of the same engine. The first connection is then closed
// with a NOTIFY outstanding, so its ACK cannot be delivered where it belongs.
// The sibling must still receive nothing: the ACK is dropped, not rerouted.
//
// This is the guard against reintroducing cross-connection routing. It fails if
// a registry, an engine, or any other grouping ever puts these two connections
// in a position to carry each other's frames.
func TestWorker_ackAlwaysReturnsOnItsOwnConnection(t *testing.T) {
	handler := func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "answer", "42")
	}

	first := startWorkerWith(t, handler)
	second := startWorkerWith(t, handler)

	handshakeAnnouncingAsync(t, first, "engine-1")
	secondReader := handshakeAnnouncingAsync(t, second, "engine-1")

	// The sibling has answered a NOTIFY of its own, so it is fully live and past
	// every point at which it could have been grouped with the first. Without
	// this its silence below would prove only that it was not ready yet.
	answerOneNotify(t, second, secondReader, 99, 99)

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 7
	notify.FrameID = 9

	if _, err := notify.Encode(first); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("closing the first connection: %v", err)
	}

	assertNoFrame(t, second, secondReader)
}

// assertNoFrame asserts that nothing arrives on conn/reader within a short
// deadline. Used to prove a negative; that no ACK was rerouted; which has no
// synchronisation signal to wait on instead.
func assertNoFrame(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}

	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("expected nothing to arrive, but a byte did")
	} else {
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("expected a read timeout, got %v", err)
		}
	}
}

// answerOneNotify drives one NOTIFY/ACK exchange to completion, proving the
// worker behind conn is past its handshake and serving.
func answerOneNotify(t *testing.T, conn net.Conn, reader *bufio.Reader, streamID, frameID uint64) {
	t.Helper()

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = streamID
	notify.FrameID = frameID

	if _, err := notify.Encode(conn); err != nil {
		t.Fatalf("writing the sync NOTIFY: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected an AGENT-ACK for the sync NOTIFY, got frame type %d", got.frameType)
	}
}

// handshakeAnnouncingAsync completes a HELLO carrying the given engine-id and a
// capabilities list naming async, so the peer offers everything failover would
// need. Returns a reader positioned after the AGENT-HELLO.
func handshakeAnnouncingAsync(t *testing.T, conn net.Conn, engineID string) *bufio.Reader {
	t.Helper()

	reader := bufio.NewReader(conn)

	hello := helloWith(t,
		kvItem{"supported-versions", "2.0"},
		kvItem{"max-frame-size", uint32(16384)},
		kvItem{"capabilities", "pipelining,async"},
		kvItem{"engine-id", engineID},
	)
	defer frame.ReleaseFrame(hello)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	return reader
}
