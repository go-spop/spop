package typeddata

import (
	"bytes"
	"net"
	"testing"
)

func TestDecodeDoesNotAliasItsInputBuffer(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"an IPv4 address", net.IP{192, 0, 2, 1}},
		{"an IPv6 address", net.ParseIP("2001:db8::1")},
		{"a binary value", []byte{1, 2, 3, 4, 5}},
		{"an empty binary value", []byte{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _, err := Encode(tc.value, make([]byte, 0))
			if err != nil {
				t.Fatalf("encoding: %v", err)
			}

			got, _, err := Decode(buf)
			if err != nil {
				t.Fatalf("decoding: %v", err)
			}

			decoded := decodedBytes(t, got)
			snapshot := bytes.Clone(decoded)

			for i := range buf {
				buf[i] = 0xFF
			}

			if !bytes.Equal(decoded, snapshot) {
				t.Fatalf("the decoded value changed with the source buffer: expected % x, got % x", snapshot, decoded)
			}
		})
	}
}

func TestDecodeReturnsTheSameBytesItWasGiven(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  []byte
	}{
		{"an IPv4 address", net.IP{192, 0, 2, 1}, []byte{192, 0, 2, 1}},
		{"a binary value", []byte{1, 2, 3, 4, 5}, []byte{1, 2, 3, 4, 5}},
		{"an empty binary value", []byte{}, []byte{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _, err := Encode(tc.value, make([]byte, 0))
			if err != nil {
				t.Fatalf("encoding: %v", err)
			}

			got, _, err := Decode(buf)
			if err != nil {
				t.Fatalf("decoding: %v", err)
			}

			decoded := decodedBytes(t, got)
			if !bytes.Equal(decoded, tc.want) {
				t.Fatalf("expected % x, got % x", tc.want, decoded)
			}

			if decoded == nil {
				t.Fatal("expected a non-nil slice")
			}
		})
	}
}

func decodedBytes(t *testing.T, v any) []byte {
	t.Helper()

	switch value := v.(type) {
	case net.IP:
		return value

	case []byte:
		return value
	}

	t.Fatalf("expected a net.IP or a []byte, got %T", v)

	return nil
}
