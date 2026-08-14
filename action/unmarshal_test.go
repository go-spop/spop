package action

import (
	"net"
	"testing"
)

func TestUnmarshalWireBytes(t *testing.T) {
	tests := []struct {
		name   string
		buf    []byte
		expect Actions
	}{
		{
			name:   "an empty list",
			buf:    []byte{},
			expect: Actions{},
		},
		{
			name: "one unset-var",
			buf: []byte{
				0x02,
				0x02,
				0x01,
				0x08, 'i', 'p', '_', 's', 'c', 'o', 'r', 'e',
			},
			expect: Actions{NewUnsetVar(ScopeSession, "ip_score")},
		},
		{
			name: "one set-var",
			buf: []byte{
				0x01,
				0x03,
				0x01,
				0x08, 'i', 'p', '_', 's', 'c', 'o', 'r', 'e',
				0x03, 0x09,
			},
			expect: Actions{NewSetVar(ScopeSession, "ip_score", uint32(9))},
		},
		{
			name: "two actions back to back",
			buf: []byte{
				0x02, 0x02, 0x00, 0x01, 'a',
				0x01, 0x03, 0x04, 0x01, 'b', 0x03, 0x07,
			},
			expect: Actions{
				NewUnsetVar(ScopeProcess, "a"),
				NewSetVar(ScopeResponse, "b", uint32(7)),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Unmarshal(tc.buf)
			if err != nil {
				t.Fatalf("unmarshalling: %v", err)
			}

			if len(got) != len(tc.expect) {
				t.Fatalf("expected %d actions, got %d", len(tc.expect), len(got))
			}

			for i := range got {
				if got[i] != tc.expect[i] {
					t.Fatalf("action %d: expected %+v, got %+v", i, tc.expect[i], got[i])
				}
			}
		})
	}
}

func TestUnmarshalRoundTripsMarshal(t *testing.T) {
	tests := []struct {
		name    string
		actions Actions
	}{
		{"a single unset-var", Actions{NewUnsetVar(ScopeTransaction, "ip_score")}},
		{"a single set-var", Actions{NewSetVar(ScopeSession, "ip_score", uint32(42))}},
		{"a string value", Actions{NewSetVar(ScopeRequest, "name", "the quick brown fox")}},
		{"a binary value", Actions{NewSetVar(ScopeResponse, "blob", []byte{1, 2, 3})}},
		{"an IPv4 value", Actions{NewSetVar(ScopeSession, "client", net.IP{192, 0, 2, 1})}},
		{"a boolean value", Actions{NewSetVar(ScopeProcess, "flag", true)}},
		{"an empty name", Actions{NewUnsetVar(ScopeSession, "")}},
		{
			"every scope in one list",
			Actions{
				NewUnsetVar(ScopeProcess, "a"),
				NewUnsetVar(ScopeSession, "b"),
				NewUnsetVar(ScopeTransaction, "c"),
				NewUnsetVar(ScopeRequest, "d"),
				NewUnsetVar(ScopeResponse, "e"),
			},
		},
		{
			"a set-var and an unset-var",
			Actions{
				NewSetVar(ScopeSession, "ip_score", uint32(9)),
				NewUnsetVar(ScopeSession, "ip_score"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf []byte

			for _, a := range tc.actions {
				var err error

				buf, err = a.Marshal(buf)
				if err != nil {
					t.Fatalf("marshalling: %v", err)
				}
			}

			got, err := Unmarshal(buf)
			if err != nil {
				t.Fatalf("unmarshalling: %v", err)
			}

			if len(got) != len(tc.actions) {
				t.Fatalf("expected %d actions, got %d", len(tc.actions), len(got))
			}

			for i := range got {
				if got[i].Type != tc.actions[i].Type || got[i].Scope != tc.actions[i].Scope || got[i].Name != tc.actions[i].Name {
					t.Fatalf("action %d: expected %+v, got %+v", i, tc.actions[i], got[i])
				}
			}
		})
	}
}

func TestUnmarshalRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
	}{
		{"an unknown action type", []byte{0x7f, 0x02, 0x01, 0x01, 'a'}},
		{"a set-var with the unset-var arg count", []byte{0x01, 0x02, 0x01, 0x01, 'a', 0x03, 0x09}},
		{"an unset-var with the set-var arg count", []byte{0x02, 0x03, 0x01, 0x01, 'a'}},
		{"an unknown scope", []byte{0x02, 0x02, 0x7f, 0x01, 'a'}},
		{"a missing arg count", []byte{0x02}},
		{"a missing scope", []byte{0x02, 0x02}},
		{"a missing name", []byte{0x02, 0x02, 0x01}},
		{"a truncated name varint", []byte{0x02, 0x02, 0x01, 0xf0}},
		{"a name longer than the buffer", []byte{0x02, 0x02, 0x01, 0x08, 'a'}},
		{"a set-var with no value", []byte{0x01, 0x03, 0x01, 0x01, 'a'}},
		{"a set-var with a truncated value", []byte{0x01, 0x03, 0x01, 0x01, 'a', 0x03}},
		{"a set-var with a truncated string value", []byte{0x01, 0x03, 0x01, 0x01, 'a', 0x08, 0x09, 'x'}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Unmarshal(tc.buf); err == nil {
				t.Fatalf("expected an error for % x, got nil", tc.buf)
			}
		})
	}
}
