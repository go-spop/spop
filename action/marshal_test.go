package action

import (
	"bytes"
	"testing"
)

// SPOE 2.0 section 3.4 gives ACTION-UNSET-VAR exactly three fields after the
// action-type byte:
//
//	ACTION-UNSET-VAR : <UNSET-VAR:1 byte><NB-ARGS:1 byte><VAR-SCOPE:1 byte><VAR-NAME>
//
// There is no value field. LIST-OF-ACTIONS carries no item count, so a receiver
// reads entries back to back until the payload is exhausted: any byte past
// VAR-NAME is read as the next entry's ACTION-TYPE.

func TestAction_Marshal_wireBytes(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		expect []byte
	}{
		{
			name:   "unset-var carries no value field",
			action: NewUnsetVar(ScopeSession, "ip_score"),
			expect: []byte{
				0x02,                                         // ACTION-TYPE: UNSET-VAR
				0x02,                                         // NB-ARGS: scope and name
				0x01,                                         // VAR-SCOPE: session
				0x08, 'i', 'p', '_', 's', 'c', 'o', 'r', 'e', // VAR-NAME
			},
		},
		{
			name:   "set-var carries its value",
			action: NewSetVar(ScopeSession, "ip_score", uint32(9)),
			expect: []byte{
				0x01,                                         // ACTION-TYPE: SET-VAR
				0x03,                                         // NB-ARGS: scope, name and value
				0x01,                                         // VAR-SCOPE: session
				0x08, 'i', 'p', '_', 's', 'c', 'o', 'r', 'e', // VAR-NAME
				0x03, 0x09, // VAR-VALUE: UINT32 typed data
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

// Two unset-var actions marshalled back to back must produce a payload a
// receiver can walk. The offset of the second entry is derived from the
// grammar, not from the encoder's own output, so a trailing byte on the first
// entry shows up as a misaligned second entry.
func TestAction_Marshal_consecutiveUnsetVarsStayAligned(t *testing.T) {
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

	// ACTION-TYPE, NB-ARGS, VAR-SCOPE, then VAR-NAME's 1-byte length prefix and
	// its single byte: five bytes, and nothing else.
	const entryLen = 5

	if len(buf) != entryLen*2 {
		t.Fatalf("expected %d bytes for two unset-var entries, got %d: % 02x", entryLen*2, len(buf), buf)
	}

	if got := buf[entryLen]; got != byte(TypeUnsetVar) {
		t.Fatalf("second entry starts with %#02x, expected ACTION-TYPE %#02x", got, byte(TypeUnsetVar))
	}
}
