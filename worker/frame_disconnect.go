package worker

import (
	"bytes"
	"fmt"

	"github.com/go-spop/spop/frame"
)

func (w *worker) sendAgentDisconnect(statusCode uint32, message string) error {
	var frameSize, n int
	var err error

	agentDisconnectFrame := frame.AcquireFrame()
	defer frame.ReleaseFrame(agentDisconnectFrame)

	agentDisconnectFrame.Type = frame.TypeAgentDisconnect
	agentDisconnectFrame.FrameID = 0
	agentDisconnectFrame.StreamID = 0
	agentDisconnectFrame.KV.Add("status-code", statusCode)
	agentDisconnectFrame.KV.Add("message", message)

	buf := &bytes.Buffer{}
	frameSize, err = agentDisconnectFrame.Encode(buf)
	if err != nil {
		return err
	}

	w.connMu.Lock()
	n, err = w.conn.Write(buf.Bytes())
	w.connMu.Unlock()
	if err != nil {
		return err
	}
	if n != frameSize {
		return fmt.Errorf("write unexpected bytes count %d, expect %d", n, frameSize)
	}

	return nil
}
