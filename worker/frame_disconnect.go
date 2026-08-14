package worker

import (
	"bytes"

	"github.com/go-spop/spop/frame"
)

func (w *worker) sendAgentDisconnect(statusCode uint32, message string) error {
	var err error

	agentDisconnectFrame := frame.AcquireFrame()
	defer frame.ReleaseFrame(agentDisconnectFrame)

	agentDisconnectFrame.Type = frame.TypeAgentDisconnect
	// Section 3.2.9: an AGENT-DISCONNECT is not attached to a stream, so its
	// STREAM-ID and FRAME-ID must be set 0.
	agentDisconnectFrame.FrameID = 0
	agentDisconnectFrame.StreamID = 0
	agentDisconnectFrame.KV.Add("status-code", statusCode)
	agentDisconnectFrame.KV.Add("message", message)

	buf := &bytes.Buffer{}
	_, err = agentDisconnectFrame.Encode(buf)
	if err != nil {
		return err
	}

	return w.conn.WriteFrame(buf.Bytes())
}
