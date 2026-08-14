package frame

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestFrameEncodeWritesTheWholeFrameInOneCall(t *testing.T) {
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

func TestFrameEncodeReportsTheAttemptedByteCount(t *testing.T) {
	f := encodableHello(t)
	defer ReleaseFrame(f)

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

func TestFrameEncodeRecordsTheFrameLength(t *testing.T) {
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

	if int(f.Len) != n-4 {
		t.Fatalf("expected Frame.Len %d to be the %d bytes written less the prefix", f.Len, n)
	}
}

func TestFrameEncodeLengthAgreesWithRead(t *testing.T) {
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

type countingWriter struct {
	buf    bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++

	return w.buf.Write(p)
}

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
