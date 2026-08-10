package message

import "testing"

// Every buffer here is a NOTIFY payload a peer can send. Decode must return an
// error for each; it must never panic.
func TestMessages_Decode_malformed(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{
			// MESSAGE-NAME length varint is truncated, so Uvarint returns -1.
			name:  "truncated name length varint",
			input: []byte{0xf0},
		},
		{
			// Declared MESSAGE-NAME length runs past the end of the payload.
			name:  "name length beyond buffer",
			input: []byte{0x20, 'a', 'b'},
		},
		{
			// Name consumes the payload, leaving no NB-ARGS byte.
			name:  "missing nb-args byte",
			input: []byte{0x01, 'a'},
		},
		{
			// NB-ARGS claims five arguments that are not present.
			name:  "nb-args exceeds payload",
			input: []byte{0x01, 'a', 0x05},
		},
		{
			// A KV name length inside the argument list runs past the end.
			name:  "argument name length beyond buffer",
			input: []byte{0x01, 'a', 0x01, 0x40, 'x'},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMessages()
			err := m.Decode(tc.input)
			if err == nil {
				t.Fatalf("expected an error, got nil (decoded %d messages)", m.Len())
			}
		})
	}
}
