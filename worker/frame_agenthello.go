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

	agentHello.KV.Add(helloKeyVersion, version)
	agentHello.KV.Add(helloKeyMaxFrameSize, agreed.maxFrameSize)
	// Section 3.2.1's capabilities are negotiated, and an agent may announce
	// fewer than the peer offers. This agent takes part in pipelining and
	// nothing else: an ACK always returns on the connection its NOTIFY arrived
	// on, and no payload is ever fragmented.
	agentHello.KV.Add(helloKeyCapabilities, capabilityPipelining)

	buf := bytes.NewBuffer(make([]byte, 0))

	_, err = agentHello.Encode(buf)
	if err != nil {
		return fmt.Errorf("marshaling error: %v", err)
	}

	return w.conn.WriteFrame(buf.Bytes())
}
