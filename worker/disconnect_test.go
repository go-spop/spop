package worker

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/payload/kv"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/varint"
)

func TestWorkerAgentDisconnectOnFrameBeforeHandshake(t *testing.T) {
	tests := []struct {
		name      string
		frameType frame.Type
	}{
		{"notify before the handshake", frame.TypeNotify},
		{"disconnect before the handshake", frame.TypeHAProxyDisconnect},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := startWorker(t)

			f := frame.AcquireFrame()
			defer frame.ReleaseFrame(f)
			f.Type = tc.frameType

			if _, err := f.Encode(conn); err != nil {
				t.Fatalf("writing the frame: %v", err)
			}

			assertAgentDisconnect(t, readAgentFrame(t, bufio.NewReader(conn)), wantStatusCodeInvalidFrame)
		})
	}
}

func TestWorkerAgentDisconnectOnUnreadableFrame(t *testing.T) {
	conn := startWorker(t)

	if _, err := conn.Write([]byte{0x00, 0x00, 0x00, 0x01, 0x01}); err != nil {
		t.Fatalf("writing the frame: %v", err)
	}

	assertAgentDisconnect(t, readAgentFrame(t, bufio.NewReader(conn)), wantStatusCodeInvalidFrame)
}

func TestWorkerHealthcheckClosesWithoutDisconnect(t *testing.T) {
	conn := startWorker(t)
	reader := bufio.NewReader(conn)

	hello := haproxyHello(t)
	defer frame.ReleaseFrame(hello)
	hello.KV.Add("healthcheck", true)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the connection to close after the healthcheck reply, got %v", err)
	}
}

func TestWorkerAgentDisconnectOnDuplicateHello(t *testing.T) {
	conn := startWorker(t)
	reader := bufio.NewReader(conn)

	first := haproxyHello(t)
	defer frame.ReleaseFrame(first)

	if _, err := first.Encode(conn); err != nil {
		t.Fatalf("writing the first HELLO: %v", err)
	}

	if got := readAgentFrame(t, reader); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO for the first HELLO, got frame type %d", got.frameType)
	}

	second := haproxyHello(t)
	defer frame.ReleaseFrame(second)

	if _, err := second.Encode(conn); err != nil {
		t.Fatalf("writing the second HELLO: %v", err)
	}

	assertAgentDisconnect(t, readAgentFrame(t, reader), wantStatusCodeInvalidFrame)
}

const wantStatusCodeInvalidFrame = uint32(4)

type agentFrame struct {
	frameType frame.Type
	streamID  uint64
	frameID   uint64
	kv        *kv.KV
}

func readAgentFrame(t *testing.T, r io.Reader) agentFrame {
	t.Helper()

	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		t.Fatalf("reading the frame length: %v", err)
	}

	body := make([]byte, binary.BigEndian.Uint32(length[:]))
	if _, err := io.ReadFull(r, body); err != nil {
		t.Fatalf("reading the frame body: %v", err)
	}

	got := agentFrame{frameType: frame.Type(body[0]), kv: kv.NewKV()}

	body = body[5:]

	var n int

	got.streamID, n = varint.Uvarint(body)
	if n < 0 {
		t.Fatal("the reply carried a truncated STREAM-ID")
	}
	body = body[n:]

	got.frameID, n = varint.Uvarint(body)
	if n < 0 {
		t.Fatal("the reply carried a truncated FRAME-ID")
	}
	body = body[n:]

	if err := got.kv.Unmarshal(body); err != nil {
		t.Fatalf("decoding the reply's KV-LIST: %v", err)
	}

	return got
}

func haproxyHello(t *testing.T) *frame.Frame {
	t.Helper()

	f := frame.AcquireFrame()
	f.Type = frame.TypeHAProxyHello
	f.KV.Add("supported-versions", "2.0")
	f.KV.Add("max-frame-size", uint32(16384))
	f.KV.Add("capabilities", "pipelining")

	return f
}

func startWorker(t *testing.T) net.Conn {
	t.Helper()

	return startWorkerWith(t, func(context.Context, *request.Request) {})
}

func startWorkerWith(t *testing.T, handler func(context.Context, *request.Request)) net.Conn {
	t.Helper()

	client, server := net.Pipe()

	go Handle(engine.NewConn(server), Config{
		Handler: handler,
		Logger:  logger.NewNop(),
	})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	t.Cleanup(func() { client.Close() })

	return client
}

func assertAgentDisconnect(t *testing.T, got agentFrame, wantCode uint32) {
	t.Helper()

	if got.frameType != frame.TypeAgentDisconnect {
		t.Fatalf("expected an AGENT-DISCONNECT, got frame type %d", got.frameType)
	}

	code, ok := got.kv.Get("status-code")
	if !ok {
		t.Fatal("the AGENT-DISCONNECT carried no status-code")
	}

	if code != wantCode {
		t.Fatalf("expected status-code %d, got %v (%T)", wantCode, code, code)
	}

	if _, ok := got.kv.Get("message"); !ok {
		t.Fatal("the AGENT-DISCONNECT carried no message")
	}

	if got.streamID != 0 || got.frameID != 0 {
		t.Fatalf("expected STREAM-ID and FRAME-ID of 0, got %d and %d", got.streamID, got.frameID)
	}
}
