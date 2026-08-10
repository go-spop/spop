package worker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/go-spop/spop/action"
	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

// Section 3.5's status code 0 is the non-error case. Asserted as the spec's own
// literal rather than the production constant, so the test pins the wire value
// independently of the code under test.
const wantStatusCodeNormal = uint32(0)

// startWorkerDraining runs a worker whose drain the test controls, handing back
// the peer's end of the pipe, the engine.Conn to poke, and the Done channel.
// The test drives those two exactly as Agent.Shutdown does.
func startWorkerDraining(t *testing.T, handler func(context.Context, *request.Request)) (net.Conn, *engine.Conn, chan struct{}) {
	t.Helper()

	client, server := net.Pipe()
	conn := engine.NewConn(server)
	done := make(chan struct{})

	go Handle(conn, Config{
		Registry: engine.NewRegistry(),
		Handler:  handler,
		Logger:   logger.NewNop(),
		Done:     done,
	})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	t.Cleanup(func() { client.Close() })

	return client, conn, done
}

// beginDrain reproduces Agent.Shutdown's ordering: close Done first, then wake
// the read loop. The flag must be set before the poke, or a loop that has not
// yet blocked re-arms its idle deadline over the wakeup.
func beginDrain(t *testing.T, conn *engine.Conn, done chan struct{}) {
	t.Helper()

	close(done)

	if err := conn.SetReadDeadline(time.Now()); err != nil {
		t.Fatalf("waking the read loop: %v", err)
	}
}

// completeDefaultHandshake runs a plain HELLO exchange and returns a reader
// positioned after the AGENT-HELLO. Named apart from framesize_test.go's
// completeHandshake, which negotiates a non-default max-frame-size.
func completeDefaultHandshake(t *testing.T, conn net.Conn) *bufio.Reader {
	t.Helper()

	reader := bufio.NewReader(conn)

	hello := haproxyHello(t)
	defer frame.ReleaseFrame(hello)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	return reader
}

// The drain finishes work already dispatched and lets its ACK out, then says
// goodbye. Section 3.2.9 requires the socket to close just after that frame.
func TestWorker_drainFinishesInFlightWorkThenSaysGoodbye(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	client, conn, done := startWorkerDraining(t, func(_ context.Context, r *request.Request) {
		close(entered)
		<-release
		r.Actions.SetVar(action.ScopeSession, "answer", "42")
	})

	reader := completeDefaultHandshake(t, client)

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 7
	notify.FrameID = 9

	if _, err := notify.Encode(client); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never ran")
	}

	beginDrain(t, conn, done)
	close(release)

	got := readAgentFrame(t, reader)

	if got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected the in-flight ACK first, got frame type %d", got.frameType)
	}

	if got.streamID != 7 || got.frameID != 9 {
		t.Fatalf("expected STREAM-ID 7 and FRAME-ID 9, got %d and %d", got.streamID, got.frameID)
	}

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeNormal)

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}
}

// A drain on a quiet connection needs no handler at all, and must still report
// status code 0 rather than the idle timeout's code 2.
func TestWorker_drainOnAQuietConnectionSaysGoodbye(t *testing.T) {
	client, conn, done := startWorkerDraining(t, func(context.Context, *request.Request) {})

	reader := completeDefaultHandshake(t, client)

	beginDrain(t, conn, done)

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeNormal)

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}
}

// Once the drain starts, a NOTIFY still on the wire is not dispatched. SPOP has
// no way to tell HAProxy "going away, finish what you have"; section 3.2.9
// makes the goodbye terminal; so the alternative is a shutdown that never
// completes under steady traffic.
func TestWorker_drainDoesNotDispatchNewNotifies(t *testing.T) {
	dispatched := make(chan struct{}, 1)

	client, conn, done := startWorkerDraining(t, func(context.Context, *request.Request) {
		dispatched <- struct{}{}
	})

	reader := completeDefaultHandshake(t, client)

	beginDrain(t, conn, done)

	// net.Pipe is unbuffered, so this write only completes if the worker reads
	// it; which is the thing being ruled out. Write from a goroutine so the
	// test does not block on its own assertion.
	go func() {
		notify := frame.AcquireFrame()
		defer frame.ReleaseFrame(notify)
		notify.Type = frame.TypeNotify
		notify.StreamID = 1
		notify.FrameID = 1

		_, _ = notify.Encode(client)
	}()

	// The goodbye arriving first is the assertion: frames are read in order, so
	// an AGENT-DISCONNECT before any ACK proves no NOTIFY was served.
	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeNormal)

	select {
	case <-dispatched:
		t.Fatal("a NOTIFY was dispatched after the drain began")
	default:
	}
}
