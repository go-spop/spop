package typeddata

import (
	"testing"
)

// The Peers varint encodes 4 bits in its first byte and 7 in each byte after,
// so a full 64-bit value needs 10 bytes. Converting a negative signed value to
// uint64 sign-extends it — int32(-1) becomes 2^64-1 — so every negative value
// takes the longest encoding there is.
//
// Encode must produce a value Decode returns unchanged, for the whole range of
// each type it accepts.

func TestEncode_numericRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		expect any
	}{
		{"int32 negative one", int32(-1), int32(-1)},
		{"int32 minimum", int32(-2147483648), int32(-2147483648)},
		{"int32 maximum", int32(2147483647), int32(2147483647)},
		{"int32 zero", int32(0), int32(0)},

		{"int64 negative", int64(-2), int64(-2)},
		{"int64 minimum", int64(-9223372036854775808), int64(-9223372036854775808)},
		{"int64 maximum", int64(9223372036854775807), int64(9223372036854775807)},

		// int encodes as INT64, so it decodes back as int64.
		{"int negative", int(-3), int64(-3)},
		{"int maximum", int(9223372036854775807), int64(9223372036854775807)},

		{"uint32 maximum", uint32(4294967295), uint32(4294967295)},
		{"uint64 maximum", ^uint64(0), ^uint64(0)},
		{"uint64 above the 8-byte varint range", uint64(1) << 60, uint64(1) << 60},

		// uint encodes as UINT64, so it decodes back as uint64.
		{"uint maximum", ^uint(0), ^uint64(0)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, n, err := Encode(tc.value, make([]byte, 0))
			if err != nil {
				t.Fatalf("Encode returned an error: %v", err)
			}

			if n != len(buf) {
				t.Fatalf("Encode reported %d bytes, wrote %d", n, len(buf))
			}

			got, decoded, err := Decode(buf)
			if err != nil {
				t.Fatalf("Decode returned an error: %v", err)
			}

			if got != tc.expect {
				t.Fatalf("round trip changed the value: got %v (%T), want %v (%T)", got, got, tc.expect, tc.expect)
			}

			if decoded != n {
				t.Fatalf("Decode consumed %d bytes, Encode wrote %d", decoded, n)
			}
		})
	}
}
