package worker

import (
	"testing"

	"github.com/go-spop/spop/frame"
)

// Section 3.2.1's capabilities are negotiated, and an agent is free to announce
// fewer than the peer offers. This agent announces exactly one, "pipelining",
// whatever HAProxy puts in its own list: the ACK for a NOTIFY always goes back
// on the connection the NOTIFY arrived on, so there is nothing "async" would be
// true of. HAProxy 3.1 removed async from its own implementation, so on any
// current peer the capability could not be negotiated even if it were offered.
func TestWorker_advertisesOnlyPipelining(t *testing.T) {
	tests := []struct {
		name  string
		items []kvItem
	}{
		{
			name: "when the peer announces async with an engine-id",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"max-frame-size", uint32(16384)},
				{"capabilities", "pipelining,async"},
				{"engine-id", "engine-1"},
			},
		},
		{
			name: "when the peer announces async without an engine-id",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"max-frame-size", uint32(16384)},
				{"capabilities", "pipelining,async"},
			},
		},
		{
			name: "when the peer announces pipelining alone",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"max-frame-size", uint32(16384)},
				{"capabilities", "pipelining"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := exchangeHello(t, helloWith(t, tc.items...))

			if got.frameType != frame.TypeAgentHello {
				t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
			}

			capabilities, ok := got.kv.Get("capabilities")
			if !ok {
				t.Fatal("the AGENT-HELLO carried no capabilities")
			}

			if capabilities != "pipelining" {
				t.Fatalf("expected capabilities %q, got %v", "pipelining", capabilities)
			}
		})
	}
}
