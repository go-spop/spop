package worker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/go-spop/spop/action"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/request"
)

func TestWorkerRejectsInboundFrameAboveNegotiatedSize(t *testing.T) {
	conn := startWorker(t)
	reader := completeHandshake(t, conn)

	if _, err := conn.Write([]byte{0x00, 0x00, 0x01, 0x2c, 0x02}); err != nil {
		t.Fatalf("writing the frame: %v", err)
	}

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeFrameTooBig)
}

func TestWorkerAcceptsInboundFrameWithinNegotiatedSize(t *testing.T) {
	conn := startWorker(t)
	reader := completeHandshake(t, conn)

	disconnect := frame.AcquireFrame()
	defer frame.ReleaseFrame(disconnect)
	disconnect.Type = frame.TypeHAProxyDisconnect
	disconnect.KV.Add("status-code", uint32(0))
	disconnect.KV.Add("message", "goodbye")

	if _, err := disconnect.Encode(conn); err != nil {
		t.Fatalf("writing the frame: %v", err)
	}

	got := readAgentFrame(t, reader)
	if got.frameType != frame.TypeAgentDisconnect {
		t.Fatalf("expected an AGENT-DISCONNECT, got frame type %d", got.frameType)
	}

	if code, _ := got.kv.Get("status-code"); code != uint32(0) {
		t.Fatalf("expected status-code 0 for a normal disconnect, got %v", code)
	}
}

func TestWorkerReportsOversizedAck(t *testing.T) {
	handler := func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "big", strings.Repeat("x", negotiatedSize*2))
	}

	conn := startWorkerWith(t, handler)
	reader := completeHandshake(t, conn)

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 1
	notify.FrameID = 1

	if _, err := notify.Encode(conn); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeFrameTooBig)

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}
}

func TestWorkerWritesAckWithinNegotiatedSize(t *testing.T) {
	handler := func(_ context.Context, r *request.Request) {
		r.Actions.SetVar(action.ScopeSession, "small", "ok")
	}

	conn := startWorkerWith(t, handler)
	reader := completeHandshake(t, conn)

	notify := frame.AcquireFrame()
	defer frame.ReleaseFrame(notify)
	notify.Type = frame.TypeNotify
	notify.StreamID = 1
	notify.FrameID = 1

	if _, err := notify.Encode(conn); err != nil {
		t.Fatalf("writing the NOTIFY: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentAck {
		t.Fatalf("expected an AGENT-ACK, got frame type %d", got.frameType)
	}
}

const wantStatusCodeFrameTooBig = uint32(3)

const negotiatedSize = 256

func completeHandshake(t *testing.T, conn net.Conn) *bufio.Reader {
	t.Helper()

	reader := bufio.NewReader(conn)

	hello := helloWith(t,
		kvItem{"supported-versions", "2.0"},
		kvItem{"max-frame-size", uint32(negotiatedSize)},
		kvItem{"capabilities", ""},
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
