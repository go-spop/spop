package varint

import (
	"bytes"
	"testing"
)

func TestPutUvarintLeavesAShortBufferUntouched(t *testing.T) {
	tests := []struct {
		name  string
		value uint64
		size  int
	}{
		{"a two-byte value in a one-byte buffer", 240, 1},
		{"a three-byte value in a two-byte buffer", 2288, 2},
		{"a five-byte value in a four-byte buffer", 4328786160, 4},
		{"the widest value in a nine-byte buffer", ^uint64(0), MaxLen - 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.Repeat([]byte{0xAA}, tc.size)
			want := bytes.Repeat([]byte{0xAA}, tc.size)

			if n := PutUvarint(buf, tc.value); n != -1 {
				t.Fatalf("expected -1 for a buffer of %d bytes, got %d", tc.size, n)
			}

			if !bytes.Equal(buf, want) {
				t.Fatalf("expected the buffer to be untouched, got % x", buf)
			}
		})
	}
}

func TestPutUvarintFillsABufferThatFitsExactly(t *testing.T) {
	tests := []struct {
		name  string
		value uint64
		size  int
	}{
		{"a one-byte value", 239, 1},
		{"a two-byte value", 240, 2},
		{"a three-byte value", 2288, 3},
		{"the widest value", ^uint64(0), MaxLen},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, tc.size)

			n := PutUvarint(buf, tc.value)
			if n != tc.size {
				t.Fatalf("expected %d bytes written, got %d", tc.size, n)
			}

			got, read := Uvarint(buf)
			if read != tc.size {
				t.Fatalf("expected %d bytes read back, got %d", tc.size, read)
			}

			if got != tc.value {
				t.Fatalf("expected %d, got %d", tc.value, got)
			}
		})
	}
}
