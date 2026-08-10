package varint

import (
	"testing"
)

// MaxLen is the contract callers size their scratch buffers from, so it has to
// be both sufficient and tight: one byte less must not be enough for the
// largest value there is.

func TestMaxLen_isSufficient(t *testing.T) {
	values := []uint64{
		0,
		239,        // last 1-byte value
		240,        // first 2-byte value
		2287, 2288, // 2/3-byte boundary
		4328786159, 4328786160, // 5-byte boundary from the spec table
		1 << 53,
		1 << 60,
		^uint64(0), // what a sign-extended negative becomes
	}

	for _, v := range values {
		buf := make([]byte, MaxLen)

		n := PutUvarint(buf, v)
		if n < 0 {
			t.Fatalf("PutUvarint(%d) reported a buffer of %d bytes as too small", v, MaxLen)
		}

		got, read := Uvarint(buf[:n])
		if read != n {
			t.Fatalf("PutUvarint(%d) wrote %d bytes, Uvarint read %d", v, n, read)
		}

		if got != v {
			t.Fatalf("round trip changed the value: got %d, want %d", got, v)
		}
	}
}

func TestMaxLen_isTight(t *testing.T) {
	if n := PutUvarint(make([]byte, MaxLen-1), ^uint64(0)); n >= 0 {
		t.Fatalf("MaxLen is larger than necessary: %d bytes encoded the maximum value in %d", MaxLen-1, n)
	}
}
