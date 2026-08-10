package frame

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// countingWriter records how many Write calls a single Encode makes, which is
// the only way to see a torn frame from outside: a prefix and a body written
// separately are indistinguishable from one write once they have both landed.
type countingWriter struct {
	buf    bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++

	return w.buf.Write(p)
}

// errAfter accepts limit bytes and then fails, standing in for a socket that
// dies mid-frame.
type errAfter struct {
	limit int
}

var errWriterFailed = errors.New("writer failed")

func (w *errAfter) Write(p []byte) (int, error) {
	if len(p) <= w.limit {
		return len(p), nil
	}

	return w.limit, errWriterFailed
}

// encodableHello builds a frame with a payload, so the body is a realistic
// length rather than the degenerate empty case. Named apart from
// read_hello_types_test.go's helloFrame, which builds raw wire bytes instead.
func encodableHello(t *testing.T) *Frame {
	t.Helper()

	f := NewFrame()
	f.Type = TypeHAProxyHello
	f.StreamID = 0
	f.FrameID = 0
	f.KV.Add("supported-versions", "2.0")
	f.KV.Add("max-frame-size", uint32(16384))

	return f
}

// A frame must reach its writer whole. Encode's signature accepts any
// io.Writer, and against a non-buffering one a separate prefix write can leave
// a 4-byte length on the wire with no body behind it.
func TestFrame_Encode_writesTheWholeFrameInOneCall(t *testing.T) {
	f := encodableHello(t)
	defer ReleaseFrame(f)

	w := &countingWriter{}

	n, err := f.Encode(w)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	if w.writes != 1 {
		t.Fatalf("expected the frame to go out in 1 write, got %d", w.writes)
	}

	if n != w.buf.Len() {
		t.Fatalf("Encode reported %d bytes, the writer received %d", n, w.buf.Len())
	}
}

// A failed write must say how much of the frame was attempted. Both messages
// reported len(f.tmp); the constant 5; which is wrong for the 4-byte prefix
// and unrelated to anything for the body.
func TestFrame_Encode_reportsTheAttemptedByteCount(t *testing.T) {
	f := encodableHello(t)
	defer ReleaseFrame(f)

	// Encode once into a buffer to learn the frame's true size, then fail the
	// real write partway through it.
	var sized bytes.Buffer
	total, err := f.Encode(&sized)
	if err != nil {
		t.Fatalf("sizing encode: %v", err)
	}

	const accepted = 3

	n, err := f.Encode(&errAfter{limit: accepted})
	if !errors.Is(err, errWriterFailed) {
		t.Fatalf("expected the writer's error to survive, got %v", err)
	}

	if n != accepted {
		t.Fatalf("expected the %d bytes actually written to be reported, got %d", accepted, n)
	}

	if want := fmt.Sprintf("expect %d", total); !strings.Contains(err.Error(), want) {
		t.Fatalf("expected the error to report %q, got %q", want, err.Error())
	}
}

// Encode computes the frame length, writes it to the wire, and must also record
// it; Read maintains the same field, and a caller reading it after an encode
// otherwise gets a stale zero.
func TestFrame_Encode_recordsTheFrameLength(t *testing.T) {
	f := encodableHello(t)
	defer ReleaseFrame(f)

	var buf bytes.Buffer

	n, err := f.Encode(&buf)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	wire := binary.BigEndian.Uint32(buf.Bytes()[0:4])

	if f.Len != wire {
		t.Fatalf("expected Frame.Len %d to match the wire prefix %d", f.Len, wire)
	}

	// FRAME-LENGTH excludes its own 4 prefix bytes, which is the same quantity
	// Read stores, so the two operations agree on what the field means.
	if int(f.Len) != n-4 {
		t.Fatalf("expected Frame.Len %d to be the %d bytes written less the prefix", f.Len, n)
	}
}

// The round trip pins the agreement directly: a frame encoded and then read
// back must report the same length on both sides.
func TestFrame_Encode_lengthAgreesWithRead(t *testing.T) {
	f := encodableHello(t)
	defer ReleaseFrame(f)

	var buf bytes.Buffer

	if _, err := f.Encode(&buf); err != nil {
		t.Fatalf("encoding: %v", err)
	}

	got := NewFrame()
	defer ReleaseFrame(got)

	if err := got.Read(&buf); err != nil {
		t.Fatalf("reading it back: %v", err)
	}

	if got.Len != f.Len {
		t.Fatalf("Read reported length %d, Encode recorded %d", got.Len, f.Len)
	}
}
