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
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/transport"
)

func TestWorkerClosesWhenAnAckCannotBeEncoded(t *testing.T) {
	conn := startWorkerWith(t, func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "answer", struct{}{})
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

	writeNotify(t, conn, 1, 1)

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeUnknown)
	assertConnectionClosed(t, conn, reader)
}

func TestWorkerClosesWhenAnAckWriteFails(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close() })

	conn := transport.NewConn(server)
	conn.SetWriteTimeout(50 * time.Millisecond)

	go Handle(conn, Config{
		Handler: func(_ context.Context, r *request.Request) {
			r.Actions.SetVar(action.ScopeSession, "answer", "42")
		},
		Logger: logger.NewNop(),
	})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	reader := bufio.NewReader(client)

	hello := haproxyHello(t)
	defer frame.ReleaseFrame(hello)

	if _, err := hello.Encode(client); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	writeNotify(t, client, 1, 1)

	waitUntilClosed(t, conn)
}

func waitUntilClosed(t *testing.T, conn *transport.Conn) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if conn.IsClosed() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected the worker to close the connection after the ACK write failed")
}

func assertConnectionClosed(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}

	for {
		_, err := reader.ReadByte()
		if err == nil {
			continue
		}

		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
			return
		}

		t.Fatalf("expected the worker to close the connection, got %v", err)
	}
}

const (
	wantStatusCodeIOError = uint32(1)
	wantStatusCodeUnknown = uint32(99)
)
