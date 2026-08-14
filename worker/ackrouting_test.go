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

func TestWorkerAckAlwaysReturnsOnItsOwnConnection(t *testing.T) {
	handler := func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "answer", "42")
	}

	first := startWorkerWith(t, handler)
	second := startWorkerWith(t, handler)

	handshakeAnnouncingAsync(t, first, "engine-1")
	secondReader := handshakeAnnouncingAsync(t, second, "engine-1")

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
