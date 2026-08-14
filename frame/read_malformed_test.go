package frame

import (
	"bytes"
	"testing"
)

func TestFrameReadMalformed(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{

			name:  "zero length prefix",
			input: []byte{0x00, 0x00, 0x00, 0x00, 0x01},
		},
		{

			name:  "length too short for flags",
			input: []byte{0x00, 0x00, 0x00, 0x01, 0x01},
		},
		{

			name:  "no room for stream id",
			input: []byte{0x00, 0x00, 0x00, 0x05, 0x01, 0x00, 0x00, 0x00, 0x01},
		},
		{

			name:  "truncated stream id varint",
			input: []byte{0x00, 0x00, 0x00, 0x06, 0x01, 0x00, 0x00, 0x00, 0x01, 0xf0},
		},
		{

			name:  "truncated frame id varint",
			input: []byte{0x00, 0x00, 0x00, 0x07, 0x01, 0x00, 0x00, 0x00, 0x01, 0x01, 0xf0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFrame()
			err := f.Read(bytes.NewReader(tc.input))
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
		})
	}
}

func TestFrameReadOversizedLengthDoesNotAllocate(t *testing.T) {
	input := []byte{0x10, 0x00, 0x00, 0x00, 0x01}

	f := NewFrame()
	allocs := testing.AllocsPerRun(1, func() {
		_ = f.Read(bytes.NewReader(input))
	})

	if allocs > 16 {
		t.Fatalf("Read allocated %v objects for a rejected oversized frame", allocs)
	}
}

func TestFrameReadLengthAboveMaxIsRejected(t *testing.T) {
	input := []byte{0xff, 0xff, 0xff, 0xff, 0x01}

	f := NewFrame()
	err := f.Read(bytes.NewReader(input))
	if err == nil {
		t.Fatal("expected an error for a frame length above the maximum, got nil")
	}
}
