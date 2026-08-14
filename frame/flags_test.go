package frame

import (
	"bytes"
	"errors"
	"testing"
)

func TestFrameEncodeRejectsAbortWithoutFin(t *testing.T) {
	f := NewFrame()
	defer ReleaseFrame(f)

	f.Type = TypeAgentAck
	f.Flags = flagAbort

	var buf bytes.Buffer

	n, err := f.Encode(&buf)
	if !errors.Is(err, ErrAbortWithoutFin) {
		t.Fatalf("expected ErrAbortWithoutFin, got %v", err)
	}

	if n != 0 {
		t.Fatalf("expected no bytes written, got %d", n)
	}

	if buf.Len() != 0 {
		t.Fatalf("expected nothing on the wire, got %d bytes", buf.Len())
	}
}

func TestFrameEncodeAcceptsAbortWithFin(t *testing.T) {
	f := NewFrame()
	defer ReleaseFrame(f)

	f.Type = TypeAgentAck
	f.Flags = flagFin | flagAbort

	var buf bytes.Buffer

	if _, err := f.Encode(&buf); err != nil {
		t.Fatalf("ABORT with FIN must encode, got %v", err)
	}

	if !f.IsAbort() || !f.IsFin() {
		t.Fatal("expected both flags to survive the encode")
	}
}

func TestFrameEncodeAcceptsFinAlone(t *testing.T) {
	f := NewFrame()
	defer ReleaseFrame(f)

	f.Type = TypeAgentAck

	var buf bytes.Buffer

	if _, err := f.Encode(&buf); err != nil {
		t.Fatalf("a default frame must encode, got %v", err)
	}
}
