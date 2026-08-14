package worker

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/transport"
)

func TestWorkerMaxInFlightBlocksTheNextNotify(t *testing.T) {
	entered := make(chan struct{}, 3)
	release := make(chan struct{}, 3)

	client, _, _ := startWorkerLimited(t, 2, func(context.Context, *request.Request) {
		entered <- struct{}{}
		<-release
	})

	reader := completeDefaultHandshake(t, client)

	writeNotify(t, client, 1, 1)
	writeNotify(t, client, 2, 2)

	awaitEntries(t, entered, 2)

	writeNotify(t, client, 3, 3)

	assertNoEntry(t, entered)

	release <- struct{}{}

	assertNoEntry(t, entered)

	readAck(t, reader)

	awaitEntries(t, entered, 1)

	release <- struct{}{}
	release <- struct{}{}

	readAck(t, reader)
	readAck(t, reader)
}

func TestWorkerMaxInFlightZeroDispatchesWithoutLimit(t *testing.T) {
	entered := make(chan struct{}, 3)
	release := make(chan struct{})

	client, _, _ := startWorkerLimited(t, 0, func(context.Context, *request.Request) {
		entered <- struct{}{}
		<-release
	})

	reader := completeDefaultHandshake(t, client)

	writeNotify(t, client, 1, 1)
	writeNotify(t, client, 2, 2)
	writeNotify(t, client, 3, 3)

	awaitEntries(t, entered, 3)

	close(release)

	readAck(t, reader)
	readAck(t, reader)
	readAck(t, reader)
}

func TestWorkerAnswersAHAProxyDisconnectWhileSaturated(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})

	client, _, _ := startWorkerLimited(t, 2, func(context.Context, *request.Request) {
		entered <- struct{}{}
		<-release
	})

	t.Cleanup(func() { close(release) })

	reader := completeDefaultHandshake(t, client)

	writeNotify(t, client, 1, 1)
	writeNotify(t, client, 2, 2)

	awaitEntries(t, entered, 2)

	disconnect := frame.AcquireFrame()
	defer frame.ReleaseFrame(disconnect)

	disconnect.Type = frame.TypeHAProxyDisconnect
	disconnect.KV.Add("status-code", uint32(0))
	disconnect.KV.Add("message", "normal")

	if _, err := disconnect.Encode(client); err != nil {
		t.Fatalf("writing the HAPROXY-DISCONNECT: %v", err)
	}

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeNormal)
}

func TestWorkerDrainAbandonsANotifyWaitingForASlot(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})

	client, conn, done := startWorkerLimited(t, 1, func(context.Context, *request.Request) {
		entered <- struct{}{}
		<-release
	})

	reader := completeDefaultHandshake(t, client)

	writeNotify(t, client, 1, 1)

	awaitEntries(t, entered, 1)

	writeNotify(t, client, 2, 2)

	beginDrain(t, conn, done)

	close(release)

	got := readAck(t, reader)
	if got.streamID != 1 || got.frameID != 1 {
		t.Fatalf("expected STREAM-ID 1 and FRAME-ID 1, got %d and %d", got.streamID, got.frameID)
	}

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeNormal)

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}

	select {
	case <-entered:
		t.Fatal("the NOTIFY waiting for a slot was dispatched during the drain")
	default:
	}
}

func startWorkerLimited(t *testing.T, maxInFlight int, handler func(context.Context, *request.Request)) (net.Conn, *transport.Conn, chan struct{}) {
	t.Helper()

	client, server := net.Pipe()
	conn := transport.NewConn(server)
	done := make(chan struct{})

	go Handle(conn, Config{
		Handler:     handler,
		Logger:      logger.NewNop(),
		Done:        done,
		MaxInFlight: maxInFlight,
	})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	t.Cleanup(func() { client.Close() })

	return client, conn, done
}

func writeNotify(t *testing.T, conn net.Conn, streamID, frameID uint64) {
	t.Helper()

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)

	notify.Type = frame.TypeNotify
	notify.StreamID = streamID
	notify.FrameID = frameID

	if _, err := notify.Encode(conn); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}
}

func awaitEntries(t *testing.T, entered <-chan struct{}, n int) {
	t.Helper()

	for i := range n {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d handlers ran", i, n)
		}
	}
}

func readAck(t *testing.T, r io.Reader) agentFrame {
	t.Helper()

	got := readAgentFrame(t, r)
	if got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected an ACK, got frame type %d", got.frameType)
	}

	return got
}

func assertNoEntry(t *testing.T, entered <-chan struct{}) {
	t.Helper()

	select {
	case <-entered:
		t.Fatal("a handler ran while the connection was at its in-flight limit")
	case <-time.After(300 * time.Millisecond):
	}
}
