package worker

import (
	"bytes"
	"fmt"

	"github.com/go-spop/spop/frame"
)

// sendAgentHello replies to a HAPROXY-HELLO. It deliberately takes nothing from
// that frame beyond what negotiate already distilled into agreed: section 3.2.5
// fixes this reply's identifiers at 0, so there is nothing left to echo.
func (w *worker) sendAgentHello(agreed negotiated) error {
	var err error

	agentHello := frame.AcquireFrame()
	defer frame.ReleaseFrame(agentHello)

	agentHello.Type = frame.TypeAgentHello
	// Section 3.2.5: an AGENT-HELLO is not attached to a stream, so its
	// STREAM-ID and FRAME-ID must be set 0. Echoing the peer's values back was
	// conformant only for as long as the peer honoured its own obligation to
	// send 0.
	agentHello.FrameID = 0
	agentHello.StreamID = 0

	agentHello.KV.Add("version", version)
	agentHello.KV.Add("max-frame-size", agreed.maxFrameSize)
	agentHello.KV.Add("capabilities", w.advertisedCapabilities())

	buf := bytes.NewBuffer(make([]byte, 0))

	_, err = agentHello.Encode(buf)
	if err != nil {
		return fmt.Errorf("marshaling error: %v", err)
	}

	// This frame describes this connection's own state, so it is never routed
	// to a sibling.
	return w.conn.WriteFrame(buf.Bytes())
}
