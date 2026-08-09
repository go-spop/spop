package typeddata

import "testing"

// Every buffer here is reachable from a peer-supplied frame body. Decode must
// return an error for each; it must never panic, and it must never report a
// zero-length successful decode, which would leave a caller's cursor parked on
// the same byte.
func TestDecode_malformed(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"int32 with no value", []byte{0x02}},
		{"uint32 with no value", []byte{0x03}},
		{"int64 with no value", []byte{0x04}},
		{"uint64 with no value", []byte{0x05}},
		{"int32 with truncated varint", []byte{0x02, 0xf0}},
		{"ipv4 with no payload", []byte{0x06}},
		{"ipv4 with truncated payload", []byte{0x06, 0x7f, 0x00}},
		{"ipv6 with no payload", []byte{0x07}},
		{"ipv6 with truncated payload", []byte{0x07, 0x01, 0x02, 0x03}},
		{"string with no length", []byte{0x08}},
		{"string with truncated length varint", []byte{0x08, 0xf0}},
		{"string with length beyond buffer", []byte{0x08, 0x10, 'a', 'b'}},
		{"binary with no length", []byte{0x09}},
		{"binary with truncated length varint", []byte{0x09, 0xf0}},
		{"binary with length beyond buffer", []byte{0x09, 0x10, 0x01}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, n, err := Decode(tc.input)
			if err == nil {
				t.Fatalf("expected an error, got nil (n=%d)", n)
			}
		})
	}
}

// A caller advances its cursor by the returned count. Reporting success while
// consuming nothing lets a malformed value be silently reinterpreted as the
// start of the next field.
func TestDecode_neverSucceedsWithoutConsuming(t *testing.T) {
	inputs := [][]byte{
		{0x02},       // int32, no value
		{0x03},       // uint32, no value
		{0x04},       // int64, no value
		{0x05},       // uint64, no value
		{0x02, 0xf0}, // int32, truncated varint
	}

	for _, in := range inputs {
		_, n, err := Decode(in)
		if err == nil && n == 0 {
			t.Fatalf("Decode(%#v) reported success while consuming 0 bytes", in)
		}
	}
}
