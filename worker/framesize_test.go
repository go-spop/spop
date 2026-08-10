package worker

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/go-spop/spop/action"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/request"
)

// SPOE 2.0 section 3.2: "Frames cannot exceed a maximum size negotiated between
// HAProxy and agents during the HELLO handshake", and section 3.2.5 says the
// announced value "will be used for all subsequent frames". Section 3.5 code 3
// is "frame is too big".
const wantStatusCodeFrameTooBig = uint32(3)

// negotiatedSize is the spec's own floor, which keeps the oversized cases small
// enough to build by hand.
const negotiatedSize = 256

// completeHandshake runs a HELLO exchange that negotiates negotiatedSize, and
// returns a reader positioned after the AGENT-HELLO.
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

// A frame whose declared length is above the negotiated ceiling must be
// refused, even though it is well under the library's own hard limit.
func TestWorker_rejectsInboundFrameAboveNegotiatedSize(t *testing.T) {
	conn := startWorker(t)
	reader := completeHandshake(t, conn)

	// FRAME-LENGTH 300, type HAPROXY-DISCONNECT: above the 256 negotiated, far
	// below frame.MaxFrameSize.
	if _, err := conn.Write([]byte{0x00, 0x00, 0x01, 0x2c, 0x02}); err != nil {
		t.Fatalf("writing the frame: %v", err)
	}

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeFrameTooBig)
}

// A frame within the negotiated ceiling still parses, so the limit is the
// negotiated value and not something stricter.
func TestWorker_acceptsInboundFrameWithinNegotiatedSize(t *testing.T) {
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

	// The normal reply to a HAPROXY-DISCONNECT: status code 0, not an error.
	got := readAgentFrame(t, reader)
	if got.frameType != frame.TypeAgentDisconnect {
		t.Fatalf("expected an AGENT-DISCONNECT, got frame type %d", got.frameType)
	}

	if code, _ := got.kv.Get("status-code"); code != uint32(0) {
		t.Fatalf("expected status-code 0 for a normal disconnect, got %v", code)
	}
}

// An ACK the handler makes too large cannot go on the wire: section 3.2.9 wants
// the error reported, and code 3 names it.
func TestWorker_reportsOversizedAck(t *testing.T) {
	handler := func(r *request.Request) {
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

	// Section 3.2.9: the agent closes the socket just after sending the frame.
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the disconnect, got %v", err)
	}
}

// An ACK that fits is written as an ACK, so the ceiling does not reject
// ordinary traffic.
func TestWorker_writesAckWithinNegotiatedSize(t *testing.T) {
	handler := func(r *request.Request) {
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
