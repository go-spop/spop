package worker

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/go-spop/spop/action"
	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

// Section 3.2.1 makes async symmetrical, the same way it makes pipelining
// symmetrical: "To be used, it must be supported by HAproxy and agents."
// engine-id is only what connections would be grouped by; HAProxy sends it
// unconditionally, whether or not async is configured, so its presence alone
// says nothing about the peer's agreement. async may only be advertised when
// BOTH an engine-id is present AND the peer's own capabilities list names
// async.
func TestWorker_advertisesAsyncOnlyWhenNegotiated(t *testing.T) {
	tests := []struct {
		name  string
		items []kvItem
		want  string
	}{
		{
			name: "with an engine-id and the peer's agreement",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"max-frame-size", uint32(16384)},
				{"capabilities", "pipelining,async"},
				{"engine-id", "engine-1"},
			},
			want: "pipelining,async",
		},
		{
			name: "without an engine-id",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"max-frame-size", uint32(16384)},
				{"capabilities", "pipelining"},
			},
			want: "pipelining",
		},
		{
			name: "with an engine-id but without the peer's agreement",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"max-frame-size", uint32(16384)},
				{"capabilities", "pipelining"},
				{"engine-id", "engine-1"},
			},
			want: "pipelining",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := exchangeHello(t, helloWith(t, tc.items...))

			if got.frameType != frame.TypeAgentHello {
				t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
			}

			capabilities, ok := got.kv.Get("capabilities")
			if !ok {
				t.Fatal("the AGENT-HELLO carried no capabilities")
			}

			if capabilities != tc.want {
				t.Fatalf("expected capabilities %q, got %v", tc.want, capabilities)
			}
		})
	}
}

// The whole point of async: HAProxy retires the connection a NOTIFY arrived on,
// and the ACK reaches it on a sibling instead of being lost.
func TestWorker_acksFailOverToASiblingConnection(t *testing.T) {
	registry := engine.NewRegistry()

	handler := func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "answer", "42")
	}

	first := startWorkerOn(t, registry, handler)
	second := startWorkerOn(t, registry, handler)

	handshakeOn(t, first, "engine-1")
	secondReader := handshakeOn(t, second, "engine-1")

	// The sibling must be in the engine before the originating connection dies,
	// or there is nothing to fail over to.
	waitUntilJoined(t, second, secondReader, 99, 99)

	// Send a NOTIFY on the first connection but do not read the reply, then
	// close that connection so its ACK cannot be delivered there.
	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 7
	notify.FrameID = 9

	if _, err := notify.Encode(first); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("closing the first connection: %v", err)
	}

	got := readAgentFrame(t, secondReader)

	if got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected an AGENT-ACK on the sibling, got frame type %d", got.frameType)
	}

	if got.streamID != 7 || got.frameID != 9 {
		t.Fatalf("expected the NOTIFY's STREAM-ID 7 and FRAME-ID 9, got %d and %d", got.streamID, got.frameID)
	}
}

// An ACK still goes out on its own connection when that connection is healthy,
// so failover has not changed the happy path. The sibling must stay silent:
// without that assertion this test would pass identically whether the sibling
// ever joined the engine at all.
func TestWorker_acksPreferTheirOwnConnection(t *testing.T) {
	registry := engine.NewRegistry()

	handler := func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "answer", "42")
	}

	first := startWorkerOn(t, registry, handler)
	second := startWorkerOn(t, registry, handler)

	firstReader := handshakeOn(t, first, "engine-1")
	secondReader := handshakeOn(t, second, "engine-1")

	// Confirm the sibling actually joined the engine, so its silence below
	// proves preference rather than the sibling simply not being ready yet.
	waitUntilJoined(t, second, secondReader, 99, 99)

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 3
	notify.FrameID = 4

	if _, err := notify.Encode(first); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	got := readAgentFrame(t, firstReader)
	if got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected the ACK on its own connection, got frame type %d", got.frameType)
	}

	if got.streamID != 3 || got.frameID != 4 {
		t.Fatalf("expected STREAM-ID 3 and FRAME-ID 4, got %d and %d", got.streamID, got.frameID)
	}

	assertNoFrame(t, second, secondReader)
}

// The regression guard for the critical finding this fixes: engine-id alone
// must never be enough to enable failover. Two connections share an
// engine-id, but neither one's HELLO names async in its capabilities, so they
// must never be grouped. If the gate on w.async were removed and Join ran off
// engineID alone, the sibling would receive the rerouted ACK and this test
// would fail.
func TestWorker_noFailoverWithoutTheAsyncCapability(t *testing.T) {
	registry := engine.NewRegistry()

	handler := func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "answer", "42")
	}

	first := startWorkerOn(t, registry, handler)
	second := startWorkerOn(t, registry, handler)

	handshakeWithCapabilities(t, first, "engine-1", "pipelining")
	secondReader := handshakeWithCapabilities(t, second, "engine-1", "pipelining")

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 7
	notify.FrameID = 9

	if _, err := notify.Encode(first); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("closing the first connection: %v", err)
	}

	assertNoFrame(t, second, secondReader)
}

// assertNoFrame asserts that nothing arrives on conn/reader within a short
// deadline. Used to prove a negative -- that no ACK was rerouted -- which has
// no synchronisation signal to wait on instead.
func assertNoFrame(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}

	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("expected nothing to arrive, but a byte did")
	} else {
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("expected a read timeout, got %v", err)
		}
	}
}

// waitUntilJoined proves the worker behind conn has completed registry.Join.
// The worker joins its engine before returning to the read loop, so a worker
// that has answered a NOTIFY is necessarily past that point. Without this the
// test races the join and the sibling may not be in the engine yet.
func waitUntilJoined(t *testing.T, conn net.Conn, reader *bufio.Reader, streamID, frameID uint64) {
	t.Helper()

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = streamID
	notify.FrameID = frameID

	if _, err := notify.Encode(conn); err != nil {
		t.Fatalf("writing the sync NOTIFY: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected an AGENT-ACK for the sync NOTIFY, got frame type %d", got.frameType)
	}
}

// startWorkerOn runs a worker sharing the given registry, so several
// connections can join one engine.
func startWorkerOn(t *testing.T, registry *engine.Registry, handler func(context.Context, *request.Request)) net.Conn {
	t.Helper()

	client, server := net.Pipe()

	go Handle(engine.NewConn(server), Config{
		Registry: registry,
		Handler:  handler,
		Logger:   logger.NewNop(),
	})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	t.Cleanup(func() { client.Close() })

	return client
}

// handshakeOn completes a HELLO carrying the given engine-id and a
// capabilities list that announces async, so the connection negotiates async
// and joins the engine. Returns a reader positioned after the AGENT-HELLO.
func handshakeOn(t *testing.T, conn net.Conn, engineID string) *bufio.Reader {
	t.Helper()

	return handshakeWithCapabilities(t, conn, engineID, "pipelining,async")
}

// handshakeWithCapabilities completes a HELLO carrying the given engine-id and
// peer capabilities list verbatim, so a test can exercise negotiation itself
// rather than assuming async. Returns a reader positioned after the
// AGENT-HELLO.
func handshakeWithCapabilities(t *testing.T, conn net.Conn, engineID, capabilities string) *bufio.Reader {
	t.Helper()

	reader := bufio.NewReader(conn)

	hello := helloWith(t,
		kvItem{"supported-versions", "2.0"},
		kvItem{"max-frame-size", uint32(16384)},
		kvItem{"capabilities", capabilities},
		kvItem{"engine-id", engineID},
	)
	defer frame.ReleaseFrame(hello)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	return reader
}
