package worker

import (
	"bufio"
	"testing"

	"github.com/go-spop/spop/frame"
)

func TestWorkerAgentHelloForcesZeroIdentifiers(t *testing.T) {
	conn := startWorker(t)
	reader := bufio.NewReader(conn)

	hello := helloWith(t,
		kvItem{"supported-versions", "2.0"},
		kvItem{"max-frame-size", uint32(16384)},
		kvItem{"capabilities", "pipelining"},
	)
	defer frame.ReleaseFrame(hello)

	hello.StreamID = 42
	hello.FrameID = 99

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	got := readAgentFrame(t, reader)

	if got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}

	if got.streamID != 0 || got.frameID != 0 {
		t.Fatalf("expected STREAM-ID 0 and FRAME-ID 0, got %d and %d", got.streamID, got.frameID)
	}
}
