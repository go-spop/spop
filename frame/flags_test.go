package frame

import (
	"bytes"
	"errors"
	"testing"
)

// SPOE 2.0 section 3.2, docs/SPOE.txt:718, on the ABORT bit: "When it is set,
// the FIN bit must also be set." Encode wrote f.Flags to the wire verbatim, so
// a caller reaching for the field directly could emit the one combination the
// spec forbids.
func TestFrame_Encode_rejectsAbortWithoutFin(t *testing.T) {
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

// ABORT with FIN is the legal pairing and must still encode.
func TestFrame_Encode_acceptsAbortWithFin(t *testing.T) {
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

// The ordinary case: FIN alone, which is what NewFrame sets.
func TestFrame_Encode_acceptsFinAlone(t *testing.T) {
	f := NewFrame()
	defer ReleaseFrame(f)

	f.Type = TypeAgentAck

	var buf bytes.Buffer

	if _, err := f.Encode(&buf); err != nil {
		t.Fatalf("a default frame must encode, got %v", err)
	}
}
