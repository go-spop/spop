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

func TestWorkerHandshakeTimeoutDisconnects(t *testing.T) {
	conn := startWorkerTimeouts(t, Timeouts{Handshake: 50 * time.Millisecond})
	reader := bufio.NewReader(conn)

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeTimeout)

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}
}

func TestWorkerIdleTimeoutDisconnects(t *testing.T) {
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

func TestWorkerHandshakeTimeoutDoesNotOutliveTheHandshake(t *testing.T) {
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

func TestWorkerZeroIdleTimeoutNeverFires(t *testing.T) {
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

const wantStatusCodeTimeout = uint32(2)

func startWorkerTimeouts(t *testing.T, timeouts Timeouts) net.Conn {
	t.Helper()

	client, server := net.Pipe()

	go Handle(engine.NewConn(server), Config{
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

	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the test's own read deadline, got %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("restoring the read deadline: %v", err)
	}
}
