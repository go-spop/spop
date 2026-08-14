package action

import (
	"bytes"
	"net"
	"testing"
)

func TestActionMarshalWireBytes(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		expect []byte
	}{
		{
			name:   "unset-var carries no value field",
			action: NewUnsetVar(ScopeSession, "ip_score"),
			expect: []byte{
				0x02,
				0x02,
				0x01,
				0x08, 'i', 'p', '_', 's', 'c', 'o', 'r', 'e',
			},
		},
		{
			name:   "set-var carries its value",
			action: NewSetVar(ScopeSession, "ip_score", uint32(9)),
			expect: []byte{
				0x01,
				0x03,
				0x01,
				0x08, 'i', 'p', '_', 's', 'c', 'o', 'r', 'e',
				0x03, 0x09,
			},
		},

		{
			name:   "set-var carries an IPv4 value",
			action: NewSetVar(ScopeSession, "client", net.ParseIP("192.0.2.1")),
			expect: []byte{
				0x01,
				0x03,
				0x01,
				0x06, 'c', 'l', 'i', 'e', 'n', 't',
				0x06, 192, 0, 2, 1,
			},
		},
		{
			name:   "set-var carries an IPv6 value",
			action: NewSetVar(ScopeSession, "client", net.ParseIP("2001:db8::1")),
			expect: []byte{
				0x01,
				0x03,
				0x01,
				0x06, 'c', 'l', 'i', 'e', 'n', 't',
				0x07,
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0x01,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.action.Marshal(make([]byte, 0))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !bytes.Equal(got, tc.expect) {
				t.Fatalf("wire bytes mismatch\n got: % 02x\nwant: % 02x", got, tc.expect)
			}
		})
	}
}

func TestActionMarshalConsecutiveUnsetVarsStayAligned(t *testing.T) {
	first := NewUnsetVar(ScopeSession, "a")
	second := NewUnsetVar(ScopeRequest, "b")

	buf, err := first.Marshal(make([]byte, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf, err = second.Marshal(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const entryLen = 5

	if len(buf) != entryLen*2 {
		t.Fatalf("expected %d bytes for two unset-var entries, got %d: % 02x", entryLen*2, len(buf), buf)
	}

	if got := buf[entryLen]; got != byte(TypeUnsetVar) {
		t.Fatalf("second entry starts with %#02x, expected ACTION-TYPE %#02x", got, byte(TypeUnsetVar))
	}
}
