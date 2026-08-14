package typeddata

import (
	"bytes"
	"errors"
	"net"
	"testing"
)

func TestEncodeIP(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want []byte
	}{
		{
			name: "a 4-byte IPv4",
			ip:   net.IP{192, 0, 2, 1},
			want: []byte{TypeIPv4, 192, 0, 2, 1},
		},
		{
			name: "an IPv4 parsed into the 16-byte v4-in-v6 form",
			ip:   net.ParseIP("192.0.2.1"),
			want: []byte{TypeIPv4, 192, 0, 2, 1},
		},
		{
			name: "the IPv4 broadcast address",
			ip:   net.IPv4bcast,
			want: []byte{TypeIPv4, 255, 255, 255, 255},
		},
		{
			name: "an IPv6 address",
			ip:   net.ParseIP("2001:db8::1"),
			want: []byte{
				TypeIPv6,
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0x01,
			},
		},
		{
			name: "the IPv6 loopback",
			ip:   net.IPv6loopback,
			want: []byte{
				TypeIPv6,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0x01,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, n, err := Encode(tc.ip, make([]byte, 0))
			if err != nil {
				t.Fatalf("encoding %v: %v", tc.ip, err)
			}

			if !bytes.Equal(buf, tc.want) {
				t.Fatalf("expected % x, got % x", tc.want, buf)
			}

			if n != len(tc.want) {
				t.Fatalf("expected a byte count of %d, got %d", len(tc.want), n)
			}
		})
	}
}

func TestEncodeIPRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want net.IP
	}{
		{"IPv4", net.IP{192, 0, 2, 1}, net.IP{192, 0, 2, 1}},
		{"IPv4 in the v4-in-v6 form", net.ParseIP("192.0.2.1"), net.IP{192, 0, 2, 1}},
		{"IPv6", net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::1")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _, err := Encode(tc.ip, make([]byte, 0))
			if err != nil {
				t.Fatalf("encoding %v: %v", tc.ip, err)
			}

			got, n, err := Decode(buf)
			if err != nil {
				t.Fatalf("decoding %v: %v", tc.ip, err)
			}

			if n != len(buf) {
				t.Fatalf("expected Decode to consume all %d bytes, it consumed %d", len(buf), n)
			}

			decoded, ok := got.(net.IP)
			if !ok {
				t.Fatalf("expected a net.IP, got %T", got)
			}

			if !decoded.Equal(tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, decoded)
			}

			if !bytes.Equal(decoded, tc.want) {
				t.Fatalf("expected the exact bytes % x, got % x", tc.want, decoded)
			}
		})
	}
}

func TestEncodeIPRejectsMalformedAddresses(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
	}{
		{"nil", nil},
		{"empty", net.IP{}},
		{"three bytes", net.IP{192, 0, 2}},
		{"five bytes", net.IP{192, 0, 2, 1, 1}},
		{"seventeen bytes", make(net.IP, 17)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Encode(tc.ip, make([]byte, 0))
			if !errors.Is(err, ErrInvalidIP) {
				t.Fatalf("expected ErrInvalidIP encoding a %d-byte net.IP, got %v", len(tc.ip), err)
			}
		})
	}
}
