package varint

import (
	"testing"
)

func TestMaxLenIsSufficient(t *testing.T) {
	values := []uint64{
		0,
		239,
		240,
		2287, 2288,
		4328786159, 4328786160,
		1 << 53,
		1 << 60,
		^uint64(0),
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

func TestMaxLenIsTight(t *testing.T) {
	if n := PutUvarint(make([]byte, MaxLen-1), ^uint64(0)); n >= 0 {
		t.Fatalf("MaxLen is larger than necessary: %d bytes encoded the maximum value in %d", MaxLen-1, n)
	}
}
