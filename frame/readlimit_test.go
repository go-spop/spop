package frame

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
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
	// length check; which is the point: the length itself was accepted.
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

// A deadline expiry has to survive Read's error formatting, or the worker
// cannot tell a timeout from a malformed frame. %v severs the chain that
// errors.Is walks; %w preserves it.
func TestFrame_Read_preservesTheErrorChain(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// A deadline already in the past makes the very first read fail.
	if err := server.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	f := NewFrame()

	err := f.Read(server)
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the deadline error to survive wrapping, got %v", err)
	}
}

// The same must hold once the header has been read and the body read fails.
func TestFrame_Read_preservesTheErrorChainOnTheBody(t *testing.T) {
	// A 5-byte header declaring a 6-byte frame, followed by 2 of the 5 body
	// bytes it promises. io.ReadFull reports a PARTIAL read as
	// ErrUnexpectedEOF; with no body bytes at all it would report plain EOF,
	// which is a different path.
	input := []byte{0x00, 0x00, 0x00, 0x06, 0x01, 0xaa, 0xbb}

	f := NewFrame()

	err := f.Read(bytes.NewReader(input))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected the body read error to survive wrapping, got %v", err)
	}
}
