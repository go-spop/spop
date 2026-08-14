package agent

import (
	"bufio"
	"context"
	"encoding/binary"
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

func TestAgentShutdownDrainsAnInFlightHandler(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	a := New(func(context.Context, *request.Request) {
		close(entered)
		<-release
	}, logger.NewNop())

	addr, served := serveOnAnEphemeralPort(t, a)

	conn, reader := dialAndHandshake(t, addr)
	sendNotify(t, conn)

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never ran")
	}

	shutdown := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		shutdown <- a.Shutdown(ctx)
	}()

	select {
	case err := <-served:
		if !errors.Is(err, ErrShutdown) {
			t.Fatalf("expected ErrShutdown from Serve, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve never returned")
	}

	close(release)

	if got := readFrameType(t, reader); got != frame.TypeAgentAck {
		t.Fatalf("expected the in-flight ACK first, got frame type %d", got)
	}

	if got := readFrameType(t, reader); got != frame.TypeAgentDisconnect {
		t.Fatalf("expected an AGENT-DISCONNECT, got frame type %d", got)
	}

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}

	select {
	case err := <-shutdown:
		if err != nil {
			t.Fatalf("expected a clean drain, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown never returned")
	}
}

func TestAgentShutdownCancelsHandlersWhenTheGracePeriodExpires(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})

	a := New(func(ctx context.Context, _ *request.Request) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
	}, logger.NewNop())

	addr, _ := serveOnAnEphemeralPort(t, a)

	conn, _ := dialAndHandshake(t, addr)
	sendNotify(t, conn)

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never ran")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the grace period to expire, got %v", err)
	}

	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler was never cancelled")
	}
}

func TestAgentShutdownDoesNotWaitForAHandlerThatIgnoresItsContext(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	t.Cleanup(func() { close(release) })

	a := New(func(context.Context, *request.Request) {
		close(entered)
		<-release
	}, logger.NewNop())

	addr, _ := serveOnAnEphemeralPort(t, a)

	conn, _ := dialAndHandshake(t, addr)
	sendNotify(t, conn)

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never ran")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the grace period to expire, got %v", err)
	}
}

func TestAgentTrackRefusesOnceDraining(t *testing.T) {
	a := New(noopHandler, logger.NewNop())

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	client, server := net.Pipe()
	defer client.Close()

	if a.track(transport.NewConn(server)) {
		t.Fatal("expected track to refuse a connection once draining")
	}
}

func TestAgentShutdownWithNoConnections(t *testing.T) {
	a := New(noopHandler, logger.NewNop())

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("expected a clean shutdown, got %v", err)
	}
}

func TestAgentShutdownIsIdempotent(t *testing.T) {
	a := New(noopHandler, logger.NewNop())

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
}

func TestAgentServeAfterShutdown(t *testing.T) {
	a := New(noopHandler, logger.NewNop())

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	if err := a.Serve(l); !errors.Is(err, ErrShutdown) {
		t.Fatalf("expected ErrShutdown, got %v", err)
	}
}

func readFrameType(t *testing.T, r io.Reader) frame.Type {
	t.Helper()

	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		t.Fatalf("reading the frame length: %v", err)
	}

	body := make([]byte, binary.BigEndian.Uint32(length[:]))
	if _, err := io.ReadFull(r, body); err != nil {
		t.Fatalf("reading the frame body: %v", err)
	}

	return frame.Type(body[0])
}

func serveOnAnEphemeralPort(t *testing.T, a *Agent) (string, <-chan error) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	served := make(chan error, 1)
	go func() { served <- a.Serve(l) }()

	return l.Addr().String(), served
}

func dialAndHandshake(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() { conn.Close() })

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	hello := frame.AcquireFrame()
	defer frame.ReleaseFrame(hello)

	hello.Type = frame.TypeHAProxyHello
	hello.KV.Add("supported-versions", "2.0")
	hello.KV.Add("max-frame-size", uint32(16384))
	hello.KV.Add("capabilities", "pipelining")

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	reader := bufio.NewReader(conn)

	if got := readFrameType(t, reader); got != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got)
	}

	return conn, reader
}

func sendNotify(t *testing.T, conn net.Conn) {
	t.Helper()

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)

	notify.Type = frame.TypeNotify
	notify.StreamID = 1
	notify.FrameID = 1

	if _, err := notify.Encode(conn); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}
}
