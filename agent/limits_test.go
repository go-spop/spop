package agent

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

func TestAgentMaxInFlightReachesTheWorker(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})

	a := New(blockingHandler(entered, release), logger.NewNop(), WithMaxInFlight(1))
	shutdownOnCleanup(t, a)

	addr, _ := serveOnAnEphemeralPort(t, a)

	conn, _ := dialAndHandshake(t, addr)

	sendNotify(t, conn)
	sendNotify(t, conn)

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first handler never ran")
	}

	select {
	case <-entered:
		t.Fatal("a second handler ran with the in-flight limit set to 1")
	case <-time.After(300 * time.Millisecond):
	}

	close(release)
}

func TestAgentMaxConnectionsServesOneAtATime(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithMaxConnections(1))
	shutdownOnCleanup(t, a)

	addr, _ := serveOnAnEphemeralPort(t, a)

	first, _ := dialAndHandshake(t, addr)

	secondConn, secondReader := dialAndSendHello(t, addr)

	assertNoReply(t, secondConn, secondReader)

	first.Close()

	if got := readFrameType(t, secondReader); got != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO once the slot freed, got frame type %d", got)
	}
}

func TestAgentShutdownWakesServeWaitingForAConnectionSlot(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithMaxConnections(1))
	shutdownOnCleanup(t, a)

	addr, served := serveOnAnEphemeralPort(t, a)

	dialAndHandshake(t, addr)

	secondConn, secondReader := dialAndSendHello(t, addr)

	assertNoReply(t, secondConn, secondReader)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	select {
	case err := <-served:
		if !errors.Is(err, ErrShutdown) {
			t.Fatalf("expected ErrShutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve never returned after the shutdown")
	}
}

func TestAgentMaxConnectionsZeroAcceptsWithoutLimit(t *testing.T) {
	a := New(noopHandler, logger.NewNop())
	shutdownOnCleanup(t, a)

	addr, _ := serveOnAnEphemeralPort(t, a)

	dialAndHandshake(t, addr)
	dialAndHandshake(t, addr)
}

func shutdownOnCleanup(t *testing.T, a *Agent) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := a.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
}

func blockingHandler(entered chan<- struct{}, release <-chan struct{}) func(context.Context, *request.Request) {
	return func(context.Context, *request.Request) {
		entered <- struct{}{}
		<-release
	}
}

func dialAndSendHello(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() { conn.Close() })

	hello := frame.AcquireFrame()
	defer frame.ReleaseFrame(hello)

	hello.Type = frame.TypeHAProxyHello
	hello.KV.Add("supported-versions", "2.0")
	hello.KV.Add("max-frame-size", uint32(16384))
	hello.KV.Add("capabilities", "pipelining")

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	return conn, bufio.NewReader(conn)
}

func assertNoReply(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}

	if _, err := reader.ReadByte(); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the test's own read deadline, got %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("restoring the read deadline: %v", err)
	}
}
