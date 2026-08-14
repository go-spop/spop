package worker

import (
	"testing"

	"github.com/go-spop/spop/frame"
)

func TestWorkerAdvertisesOnlyPipelining(t *testing.T) {
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
