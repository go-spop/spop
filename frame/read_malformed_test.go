package frame

import (
	"bytes"
	"testing"
)

// Every case here is a frame a peer can send before the HELLO handshake has
// completed. Frame.Read must return an error for each; it must never panic and
// must never size an allocation from an unvalidated length prefix.

func TestFrame_Read_malformed(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{
			// FRAME-LENGTH 0 underflows f.Len-1 to 4294967295.
			name:  "zero length prefix",
			input: []byte{0x00, 0x00, 0x00, 0x00, 0x01},
		},
		{
			// FRAME-LENGTH 1 leaves no room for the 4-byte FLAGS field.
			name:  "length too short for flags",
			input: []byte{0x00, 0x00, 0x00, 0x01, 0x01},
		},
		{
			// FLAGS present but STREAM-ID missing entirely.
			name:  "no room for stream id",
			input: []byte{0x00, 0x00, 0x00, 0x05, 0x01, 0x00, 0x00, 0x00, 0x01},
		},
		{
			// STREAM-ID is a multi-byte varint whose continuation is truncated,
			// so varint.Uvarint returns n = -1.
			name:  "truncated stream id varint",
			input: []byte{0x00, 0x00, 0x00, 0x06, 0x01, 0x00, 0x00, 0x00, 0x01, 0xf0},
		},
		{
			// STREAM-ID parses, FRAME-ID continuation is truncated.
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

// A declared length far larger than the payload must not be allocated before
// the body has been read. The reader supplies only 5 bytes, so a conforming
// implementation rejects the frame without reserving 256 MiB.
func TestFrame_Read_oversizedLengthDoesNotAllocate(t *testing.T) {
	input := []byte{0x10, 0x00, 0x00, 0x00, 0x01}

	f := NewFrame()
	allocs := testing.AllocsPerRun(1, func() {
		_ = f.Read(bytes.NewReader(input))
	})

	// The guard should reject on the length alone. Allow a small allocation
	// budget for the error value and the reader, but nothing near the 256 MiB
	// the prefix declares.
	if allocs > 16 {
		t.Fatalf("Read allocated %v objects for a rejected oversized frame", allocs)
	}
}

// The frame length prefix must be rejected when it exceeds the largest frame
// the protocol permits, rather than being trusted as an allocation size.
func TestFrame_Read_lengthAboveMaxIsRejected(t *testing.T) {
	input := []byte{0xff, 0xff, 0xff, 0xff, 0x01}

	f := NewFrame()
	err := f.Read(bytes.NewReader(input))
	if err == nil {
		t.Fatal("expected an error for a frame length above the maximum, got nil")
	}
}
