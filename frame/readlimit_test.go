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

func TestFrameReadLimitRejectsAboveTheLimit(t *testing.T) {
	f := NewFrame()

	err := f.ReadLimit(bytes.NewReader(header(300, TypeHAProxyHello)), 256, nil)
	if !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("expected ErrFrameTooBig, got %v", err)
	}
}

func TestFrameReadLimitAcceptsTheLimitItself(t *testing.T) {
	f := NewFrame()

	err := f.ReadLimit(bytes.NewReader(header(256, TypeHAProxyHello)), 256, nil)
	if errors.Is(err, ErrFrameTooBig) {
		t.Fatal("a frame of exactly the limit was rejected as too big")
	}
}

func TestFrameReadLimitCapsTheLimitAtMaxFrameSize(t *testing.T) {
	f := NewFrame()

	err := f.ReadLimit(bytes.NewReader(header(MaxFrameSize+1, TypeHAProxyHello)), ^uint32(0), nil)
	if !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("expected a limit above MaxFrameSize to be capped, got %v", err)
	}
}

func TestFrameReadUsesMaxFrameSize(t *testing.T) {
	f := NewFrame()

	err := f.Read(bytes.NewReader(header(MaxFrameSize+1, TypeHAProxyHello)))
	if !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("expected ErrFrameTooBig, got %v", err)
	}
}

func TestFrameReadPreservesTheErrorChain(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	if err := server.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	f := NewFrame()

	err := f.Read(server)
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the deadline error to survive wrapping, got %v", err)
	}
}

func TestFrameReadPreservesTheErrorChainOnTheBody(t *testing.T) {

	input := []byte{0x00, 0x00, 0x00, 0x06, 0x01, 0xaa, 0xbb}

	f := NewFrame()

	err := f.Read(bytes.NewReader(input))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected the body read error to survive wrapping, got %v", err)
	}
}

func header(length uint32, frameType Type) []byte {
	return []byte{
		byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length),
		byte(frameType),
	}
}
