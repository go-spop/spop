package worker

import (
	"bufio"
	"context"
	"testing"
	"time"

	"github.com/go-spop/spop/action"
	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/request"
)

// A handler whose connection has gone away is computing a result nobody can
// receive. The worker cancels its context as it exits, which is the handler's
// cue to stop.
func TestWorker_cancelsHandlersWhenTheConnectionGoes(t *testing.T) {
	entered := make(chan struct{})
	stopped := make(chan struct{})

	conn := startWorkerWith(t, func(ctx context.Context, _ *request.Request) {
		close(entered)
		<-ctx.Done()
		close(stopped)
	})

	reader := bufio.NewReader(conn)

	hello := haproxyHello(t)
	defer frame.ReleaseFrame(hello)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 1
	notify.FrameID = 1

	if _, err := notify.Encode(conn); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never ran")
	}

	// The peer goes away: the worker's read fails, run unwinds, and the
	// connection context is cancelled.
	if err := conn.Close(); err != nil {
		t.Fatalf("closing the connection: %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler was never cancelled")
	}
}

// A handler must not be cancelled while a sibling can still carry its ACK --
// that failover is the whole point of async. The worker therefore waits for its
// in-flight handlers before cancelling, whenever a sibling remains.
func TestWorker_doesNotCancelWhileASiblingCanDeliver(t *testing.T) {
	registry := engine.NewRegistry()

	entered := make(chan struct{})
	release := make(chan struct{})
	cancelled := make(chan struct{}, 1)

	handler := func(ctx context.Context, r *request.Request) {
		close(entered)

		select {
		case <-ctx.Done():
			cancelled <- struct{}{}
			return
		case <-release:
		}

		r.Actions.SetVar(action.ScopeSession, "answer", "42")
	}

	// The sibling only needs to carry the ACK once the handler releases; its own
	// handler must return immediately, or waitUntilJoined's own synchronous
	// NOTIFY below -- answered on this same connection -- would block on the
	// very release gate this test does not close until the end.
	sibling := func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "answer", "42")
	}

	first := startWorkerOn(t, registry, handler)
	second := startWorkerOn(t, registry, sibling)

	handshakeOn(t, first, "engine-1")
	secondReader := handshakeOn(t, second, "engine-1")

	// The sibling must be in the engine before the originating connection dies,
	// or there is nothing to fail over to.
	waitUntilJoined(t, second, secondReader, 99, 99)

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 7
	notify.FrameID = 9

	if _, err := notify.Encode(first); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never ran")
	}

	// The originating connection dies while the handler is still working. A
	// sibling remains, so the ACK is still deliverable.
	if err := first.Close(); err != nil {
		t.Fatalf("closing the first connection: %v", err)
	}

	// Give the exit path several times over what it needs to cancel wrongly.
	// The assertion is on WHAT happened -- no cancellation -- not on timing.
	select {
	case <-cancelled:
		t.Fatal("the handler was cancelled while a sibling could still carry its ACK")
	case <-time.After(300 * time.Millisecond):
	}

	close(release)

	got := readAgentFrame(t, secondReader)

	if got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected an AGENT-ACK on the sibling, got frame type %d", got.frameType)
	}

	if got.streamID != 7 || got.frameID != 9 {
		t.Fatalf("expected STREAM-ID 7 and FRAME-ID 9, got %d and %d", got.streamID, got.frameID)
	}
}
