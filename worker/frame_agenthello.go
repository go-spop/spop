package worker

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/internal/spec"
)

func (w *worker) sendAgentHello(haproxyHello *frame.Frame) error {
	var err error
	var frameSize, n int

	agentHello := frame.AcquireFrame()
	defer frame.ReleaseFrame(agentHello)

	agentHello.Type = frame.TypeAgentHello
	agentHello.FrameID = haproxyHello.FrameID
	agentHello.StreamID = haproxyHello.StreamID

	supportedVersions, ok := haproxyHello.KV.Get("supported-versions")
	if !ok {
		return fmt.Errorf("HAProxy hello missing supported-versions")
	}
	sv, ok := supportedVersions.(string)
	if !ok {
		return fmt.Errorf("supported-versions is not a string")
	}
	versionSupported := false
	for _, v := range strings.Split(sv, ",") {
		if strings.TrimSpace(v) == spec.Version20 {
			versionSupported = true
			break
		}
	}
	if !versionSupported {
		return fmt.Errorf("unsupported versions: %s", sv)
	}

	agentHello.KV.Add("version", spec.Version20)

	maxFrameSize := haproxyHello.MaxFrameSize
	if maxFrameSize == 0 || maxFrameSize > spec.DefaultMaxFrameSize {
		maxFrameSize = spec.DefaultMaxFrameSize
	}
	if maxFrameSize < spec.MinFrameSize {
		return fmt.Errorf("max-frame-size %d below minimum %d", maxFrameSize, spec.MinFrameSize)
	}
	w.maxFrameSize = maxFrameSize

	agentHello.KV.Add("max-frame-size", maxFrameSize)
	agentHello.KV.Add("capabilities", capabilities)

	buf := bytes.NewBuffer(make([]byte, 0))

	frameSize, err = agentHello.Encode(buf)
	if err != nil {
		return fmt.Errorf("marshaling error: %v", err)
	}

	w.connMu.Lock()
	n, err = w.conn.Write(buf.Bytes())
	w.connMu.Unlock()
	if err != nil {
		return fmt.Errorf("error write to connection: %v", err)
	}
	if n != frameSize {
		return fmt.Errorf("write unexpected bytes count %d, expect %d", n, frameSize)
	}

	return nil
}
