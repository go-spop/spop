package message

import "testing"

func TestMessagesDecodeMalformed(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{

			name:  "truncated name length varint",
			input: []byte{0xf0},
		},
		{

			name:  "name length beyond buffer",
			input: []byte{0x20, 'a', 'b'},
		},
		{

			name:  "missing nb-args byte",
			input: []byte{0x01, 'a'},
		},
		{

			name:  "nb-args exceeds payload",
			input: []byte{0x01, 'a', 0x05},
		},
		{

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
