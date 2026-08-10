package frame

import (
	"bytes"
	"errors"
	"testing"
)

// header builds the 5 bytes Read consumes before it sizes anything: the
// FRAME-LENGTH prefix and the FRAME-TYPE.
func header(length uint32, frameType Type) []byte {
	return []byte{
		byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length),
		byte(frameType),
	}
}

func TestFrame_ReadLimit_rejectsAboveTheLimit(t *testing.T) {
	f := NewFrame()

	err := f.ReadLimit(bytes.NewReader(header(300, TypeHAProxyHello)), 256)
	if !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("expected ErrFrameTooBig, got %v", err)
	}
}

// A frame at exactly the limit is legal, so the comparison is not off by one.
func TestFrame_ReadLimit_acceptsTheLimitItself(t *testing.T) {
	f := NewFrame()

	// The body is absent, so this fails on the truncated read rather than the
	// length check -- which is the point: the length itself was accepted.
	err := f.ReadLimit(bytes.NewReader(header(256, TypeHAProxyHello)), 256)
	if errors.Is(err, ErrFrameTooBig) {
		t.Fatal("a frame of exactly the limit was rejected as too big")
	}
}

// MaxFrameSize is an absolute ceiling: a caller passing a larger limit must not
// be able to reopen the unbounded allocation the constant exists to prevent.
func TestFrame_ReadLimit_capsTheLimitAtMaxFrameSize(t *testing.T) {
	f := NewFrame()

	err := f.ReadLimit(bytes.NewReader(header(MaxFrameSize+1, TypeHAProxyHello)), ^uint32(0))
	if !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("expected a limit above MaxFrameSize to be capped, got %v", err)
	}
}

// Read is ReadLimit at the library's own ceiling.
func TestFrame_Read_usesMaxFrameSize(t *testing.T) {
	f := NewFrame()

	err := f.Read(bytes.NewReader(header(MaxFrameSize+1, TypeHAProxyHello)))
	if !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("expected ErrFrameTooBig, got %v", err)
	}
}
