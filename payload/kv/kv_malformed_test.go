package kv

import "testing"

func TestKVUnmarshalMalformed(t *testing.T) {
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
			input: []byte{0x10, 'a'},
		},
		{

			name:  "name without value",
			input: []byte{0x01, 'a'},
		},
		{

			name:  "truncated string value",
			input: []byte{0x01, 'a', 0x08, 0x10, 'b'},
		},
		{

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

func TestKVUnmarshalNBMalformed(t *testing.T) {
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
