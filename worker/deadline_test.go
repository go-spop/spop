package worker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

// SPOE 2.0 section 3.5 reserves status code 2 for "a timeout occurred". Nothing
// in the library could emit it before, because nothing set a timeout.
const wantStatusCodeTimeout = uint32(2)

// startWorkerTimeouts runs a worker with the given deadlines and hands the test
// the other end of the pipe.
func startWorkerTimeouts(t *testing.T, timeouts Timeouts) net.Conn {
	t.Helper()

	client, server := net.Pipe()

	go Handle(engine.NewConn(server), Config{
		Registry: engine.NewRegistry(),
		Handler:  func(context.Context, *request.Request) {},
		Logger:   logger.NewNop(),
		Timeouts: timeouts,
	})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	t.Cleanup(func() { client.Close() })

	return client
}

// A peer that connects and says nothing must not hold a worker forever.
func TestWorker_handshakeTimeoutDisconnects(t *testing.T) {
	conn := startWorkerTimeouts(t, Timeouts{Handshake: 50 * time.Millisecond})
	reader := bufio.NewReader(conn)

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeTimeout)

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}
}

// After a completed handshake the idle timeout takes over, and its expiry is
// the agent-initiated close section 2.2's "timeout idle" waits for.
func TestWorker_idleTimeoutDisconnects(t *testing.T) {
	conn := startWorkerTimeouts(t, Timeouts{Idle: 50 * time.Millisecond})
	reader := bufio.NewReader(conn)

	hello := haproxyHello(t)
	defer frame.ReleaseFrame(hello)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeTimeout)

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}
}

// The handshake deadline must not survive the handshake. With a short handshake
// timeout and idle disabled, a connection that completed its HELLO stays up.
func TestWorker_handshakeTimeoutDoesNotOutliveTheHandshake(t *testing.T) {
	conn := startWorkerTimeouts(t, Timeouts{Handshake: 50 * time.Millisecond})
	reader := bufio.NewReader(conn)

	hello := haproxyHello(t)
	defer frame.ReleaseFrame(hello)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	assertStaysOpen(t, conn, reader)
}

// Zero disables. A quiet connection with no idle timeout is not closed.
func TestWorker_zeroIdleTimeoutNeverFires(t *testing.T) {
	conn := startWorkerTimeouts(t, Timeouts{})
	reader := bufio.NewReader(conn)

	hello := haproxyHello(t)
	defer frame.ReleaseFrame(hello)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	assertStaysOpen(t, conn, reader)
}

// assertStaysOpen gives a short-timeout worker several times its deadline to
// misbehave, then confirms nothing arrived and the connection is still usable.
// The assertion is on WHAT happened -- no frame, no EOF -- not on timing.
func assertStaysOpen(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}

	_, err := reader.ReadByte()

	if errors.Is(err, io.EOF) {
		t.Fatal("the connection was closed when no timeout should have fired")
	}

	if err == nil {
		t.Fatal("a frame arrived when no timeout should have fired")
	}

	// The pass case is the TEST's own read deadline and nothing else. Accepting
	// any non-EOF error here would let an unrelated failure -- a reset, a
	// closed pipe -- masquerade as the connection staying healthy.
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the test's own read deadline, got %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("restoring the read deadline: %v", err)
	}
}
