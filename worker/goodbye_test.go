package worker

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/transport"
)

func TestWorkerWritesNothingAfterTheAgentDisconnect(t *testing.T) {
	client, w := newObservedWorker(t)
	reader := bufio.NewReader(client)

	sent := make(chan struct{})
	go func() {
		defer close(sent)
		w.disconnect(statusCodeNormal, "connection closed by server")
	}()

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeNormal)
	<-sent

	ack := frame.AcquireFrame()
	defer frame.ReleaseFrame(ack)
	ack.Type = frame.TypeAgentAck
	ack.StreamID = 7
	ack.FrameID = 9

	if err := w.writeFrame(ack); !errors.Is(err, errAfterDisconnect) {
		t.Fatalf("expected errAfterDisconnect, got %v", err)
	}

	assertNoFrame(t, client, reader)
}

func TestWorkerWritesAnAckBeforeTheAgentDisconnect(t *testing.T) {
	client, w := newObservedWorker(t)
	reader := bufio.NewReader(client)

	ack := frame.AcquireFrame()
	defer frame.ReleaseFrame(ack)
	ack.Type = frame.TypeAgentAck
	ack.StreamID = 7
	ack.FrameID = 9

	written := make(chan error, 1)
	go func() { written <- w.writeFrame(ack) }()

	got := readAgentFrame(t, reader)
	if got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected an AGENT-ACK, got frame type %d", got.frameType)
	}

	if got.streamID != 7 || got.frameID != 9 {
		t.Fatalf("expected STREAM-ID 7 and FRAME-ID 9, got %d and %d", got.streamID, got.frameID)
	}

	if err := <-written; err != nil {
		t.Fatalf("writing the ACK: %v", err)
	}
}

func newObservedWorker(t *testing.T) (net.Conn, *worker) {
	t.Helper()

	client, server := net.Pipe()
	t.Cleanup(func() { client.Close() })

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	conn := transport.NewConn(server)
	conn.SetWriteTimeout(2 * time.Second)

	return client, &worker{
		conn:    conn,
		handler: func(context.Context, *request.Request) {},
		logger:  logger.NewNop(),
		ctx:     ctx,
		cancel:  cancel,
	}
}
