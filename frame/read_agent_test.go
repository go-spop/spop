package frame

import (
	"bytes"
	"testing"

	"github.com/go-spop/spop/action"
)

func TestFrameReadAgentHello(t *testing.T) {
	f := NewFrame()
	f.Type = TypeAgentHello
	f.KV.Add("version", "2.0")
	f.KV.Add("max-frame-size", uint32(16384))
	f.KV.Add("capabilities", "pipelining")

	buf := &bytes.Buffer{}
	if _, err := f.Encode(buf); err != nil {
		t.Fatalf("encoding: %v", err)
	}

	got := NewFrame()
	if err := got.Read(buf); err != nil {
		t.Fatalf("reading: %v", err)
	}

	if got.Type != TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.Type)
	}

	version, ok := got.KV.Get("version")
	if !ok {
		t.Fatal("the AGENT-HELLO carried no version")
	}

	if version != "2.0" {
		t.Fatalf("expected version 2.0, got %v", version)
	}

	capabilities, ok := got.KV.Get("capabilities")
	if !ok {
		t.Fatal("the AGENT-HELLO carried no capabilities")
	}

	if capabilities != "pipelining" {
		t.Fatalf("expected capabilities pipelining, got %v", capabilities)
	}
}

func TestFrameReadAgentDisconnect(t *testing.T) {
	f := NewFrame()
	f.Type = TypeAgentDisconnect
	f.KV.Add("status-code", uint32(3))
	f.KV.Add("message", "frame is too big")

	buf := &bytes.Buffer{}
	if _, err := f.Encode(buf); err != nil {
		t.Fatalf("encoding: %v", err)
	}

	got := NewFrame()
	if err := got.Read(buf); err != nil {
		t.Fatalf("reading: %v", err)
	}

	code, ok := got.KV.Get("status-code")
	if !ok {
		t.Fatal("the AGENT-DISCONNECT carried no status-code")
	}

	if code != uint32(3) {
		t.Fatalf("expected status-code 3, got %v", code)
	}
}

func TestFrameReadAgentAck(t *testing.T) {
	f := NewFrame()
	f.Type = TypeAgentAck
	f.StreamID = 7
	f.FrameID = 9
	f.Actions = action.Actions{
		action.NewSetVar(action.ScopeSession, "ip_score", uint32(42)),
		action.NewUnsetVar(action.ScopeRequest, "stale"),
	}

	buf := &bytes.Buffer{}
	if _, err := f.Encode(buf); err != nil {
		t.Fatalf("encoding: %v", err)
	}

	got := NewFrame()
	if err := got.Read(buf); err != nil {
		t.Fatalf("reading: %v", err)
	}

	if got.StreamID != 7 || got.FrameID != 9 {
		t.Fatalf("expected STREAM-ID 7 and FRAME-ID 9, got %d and %d", got.StreamID, got.FrameID)
	}

	if len(got.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(got.Actions))
	}

	if got.Actions[0].Type != action.TypeSetVar || got.Actions[0].Name != "ip_score" || got.Actions[0].Value != uint32(42) {
		t.Fatalf("unexpected first action: %+v", got.Actions[0])
	}

	if got.Actions[1].Type != action.TypeUnsetVar || got.Actions[1].Name != "stale" {
		t.Fatalf("unexpected second action: %+v", got.Actions[1])
	}
}

func TestFrameReadAgentAckWithMalformedActions(t *testing.T) {
	body := []byte{byte(TypeAgentAck), 0, 0, 0, 1, 7, 9, 0x7f, 0x02, 0x01}

	buf := &bytes.Buffer{}
	buf.Write([]byte{0, 0, 0, byte(len(body))})
	buf.Write(body)

	got := NewFrame()
	if err := got.Read(buf); err == nil {
		t.Fatal("expected an error for a malformed LIST-OF-ACTIONS, got nil")
	}
}
