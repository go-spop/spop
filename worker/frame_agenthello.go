package worker

import (
	"bytes"
	"fmt"

	"github.com/go-spop/spop/frame"
)

func (w *worker) sendAgentHello(haproxyHello *frame.Frame, agreed negotiated) error {
	var err error

	agentHello := frame.AcquireFrame()
	defer frame.ReleaseFrame(agentHello)

	agentHello.Type = frame.TypeAgentHello
	agentHello.FrameID = haproxyHello.FrameID
	agentHello.StreamID = haproxyHello.StreamID

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
