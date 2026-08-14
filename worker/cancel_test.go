package worker

import (
	"bufio"
	"context"
	"testing"
	"time"

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
