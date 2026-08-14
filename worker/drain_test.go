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
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/transport"
)

func TestWorkerDrainFinishesInFlightWorkThenSaysGoodbye(t *testing.T) {
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

func TestWorkerDrainOnAQuietConnectionSaysGoodbye(t *testing.T) {
	client, conn, done := startWorkerDraining(t, func(context.Context, *request.Request) {})

	reader := completeDefaultHandshake(t, client)

	beginDrain(t, conn, done)

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeNormal)

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}
}

func TestWorkerDrainDoesNotDispatchNewNotifies(t *testing.T) {
	dispatched := make(chan struct{}, 1)

	client, conn, done := startWorkerDraining(t, func(context.Context, *request.Request) {
		dispatched <- struct{}{}
	})

	reader := completeDefaultHandshake(t, client)

	beginDrain(t, conn, done)

	go func() {
		notify := frame.AcquireFrame()
		defer frame.ReleaseFrame(notify)
		notify.Type = frame.TypeNotify
		notify.StreamID = 1
		notify.FrameID = 1

		_, _ = notify.Encode(client)
	}()

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeNormal)

	select {
	case <-dispatched:
		t.Fatal("a NOTIFY was dispatched after the drain began")
	default:
	}
}

const wantStatusCodeNormal = uint32(0)

func startWorkerDraining(t *testing.T, handler func(context.Context, *request.Request)) (net.Conn, *transport.Conn, chan struct{}) {
	t.Helper()

	return startWorkerLimited(t, 0, handler)
}

func beginDrain(t *testing.T, conn *transport.Conn, done chan struct{}) {
	t.Helper()

	close(done)

	if err := conn.SetReadDeadline(time.Now()); err != nil {
		t.Fatalf("waking the read loop: %v", err)
	}
}

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
