package worker

import (
	"bytes"

	"github.com/go-spop/spop/frame"
)

// sendAgentDisconnect writes the last frame this connection will carry.
//
// The latch is set before the write and under the same lock an ACK takes, so
// section 3.2.9's "nothing after the AGENT-DISCONNECT" holds against a NOTIFY
// handler finishing on another goroutine. It is set even if the write below
// fails: the connection has committed to closing either way, and an ACK sent
// after a failed goodbye is no more deliverable than one sent after a good one.
func (w *worker) sendAgentDisconnect(statusCode uint32, message string) error {
	w.sendMu.Lock()
	defer w.sendMu.Unlock()

	w.goodbyeSent = true

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
