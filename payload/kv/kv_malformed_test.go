package kv

import "testing"

// Every buffer here is a KV-LIST a peer can send in a HAPROXY-HELLO or
// HAPROXY-DISCONNECT body. Unmarshal must return an error for each; it must
// never panic.
func TestKV_Unmarshal_malformed(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{
			// KV-NAME length varint is truncated, so Uvarint returns -1.
			name:  "truncated name length varint",
			input: []byte{0xf0},
		},
		{
			// Declared KV-NAME length runs past the end of the buffer.
			name:  "name length beyond buffer",
			input: []byte{0x10, 'a'},
		},
		{
			// Name consumes the buffer, leaving no TYPED-DATA value.
			name:  "name without value",
			input: []byte{0x01, 'a'},
		},
		{
			// Value is a STRING whose declared length exceeds what remains.
			name:  "truncated string value",
			input: []byte{0x01, 'a', 0x08, 0x10, 'b'},
		},
		{
			// Value is an INT32 with no varint payload at all.
			name:  "numeric value with no payload",
			input: []byte{0x01, 'a', 0x02},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := NewKV()
			err := k.Unmarshal(tc.input)
			if err == nil {
				t.Fatalf("expected an error, got nil (decoded %d items)", len(k.Data()))
			}
		})
	}
}

// UnmarshalNB is the path message decoding uses. It must reject the same
// inputs, and must never report consuming more bytes than it was given.
func TestKV_UnmarshalNB_malformed(t *testing.T) {
	tests := []struct {
		name  string
		count int
		input []byte
	}{
		{"truncated name length varint", 1, []byte{0xf0}},
		{"name length beyond buffer", 1, []byte{0x10, 'a'}},
		{"name without value", 1, []byte{0x01, 'a'}},
		{"numeric value with no payload", 1, []byte{0x01, 'a', 0x02}},
		{"fewer pairs than count claims", 3, []byte{0x01, 'a', 0x08, 0x01, 'b'}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := NewKV()
			read, err := k.UnmarshalNB(tc.input, tc.count)
			if err == nil {
				t.Fatalf("expected an error, got nil (read %d bytes)", read)
			}
			if read > len(tc.input) {
				t.Fatalf("reported consuming %d bytes from a %d byte buffer", read, len(tc.input))
			}
		})
	}
}
