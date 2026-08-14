package client

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"

	"github.com/go-spop/spop/frame"
)

// Client is a simple client for spop protocol, this should only be used for testing purpose
type Client struct {
	conn   net.Conn
	reader io.Reader
}

// NewClient create a new Client for an established connection
func NewClient(conn net.Conn) Client {
	return Client{conn: conn, reader: bufio.NewReader(conn)}
}

// Init initialize the client by sending the HAProxyHello frame
func (c *Client) Init() error {
	f := frame.AcquireFrame()
	defer frame.ReleaseFrame(f)
	f.Type = frame.TypeHAProxyHello
	f.StreamID = 0
	f.FrameID = 0
	// Section 3.2.4 requires the "Major.Minor" format, not a bare major.
	f.KV.Add("supported-versions", "2.0")
	f.KV.Add("max-frame-size", uint32(16*1024))
	f.KV.Add("capabilities", "")

	err := c.send(f)
	if err != nil {
		return err
	}

	responseFrame := frame.AcquireFrame()
	defer frame.ReleaseFrame(responseFrame)
	if err := responseFrame.ReadLimit(c.reader, frame.MaxFrameSize, frame.FromAgent); err != nil {
		return fmt.Errorf("error read AgentHello frame: %w", err)
	}

	switch responseFrame.Type {
	case frame.TypeAgentHello:
		if responseFrame.FrameID != uint64(0) || responseFrame.StreamID != uint64(0) {
			return fmt.Errorf("FrameID or StreamID mismatch")
		}
	default:
		return fmt.Errorf("unexpected frame type: %v", responseFrame.Type)
	}

	return nil

}

func (c *Client) send(f *frame.Frame) error {
	buf := bytes.NewBuffer(make([]byte, 0))
	if _, err := f.Encode(buf); err != nil {
		return err
	}

	n, err := c.conn.Write(buf.Bytes())
	if err != nil {
		return err
	}
	if n != buf.Len() {
		return fmt.Errorf("size mismatch")
	}
	return nil
}

// Notify send an empty Notify frame
func (c *Client) Notify() error {
	f := frame.AcquireFrame()
	defer frame.ReleaseFrame(f)
	f.Type = frame.TypeNotify
	f.StreamID = 1
	f.FrameID = 1

	err := c.send(f)
	if err != nil {
		return err
	}

	responseFrame := frame.AcquireFrame()
	defer frame.ReleaseFrame(responseFrame)
	if err := responseFrame.ReadLimit(c.reader, frame.MaxFrameSize, frame.FromAgent); err != nil {
		return fmt.Errorf("error read AgentAck frame: %w", err)
	}

	if responseFrame.Type != frame.TypeAgentAck {
		return fmt.Errorf("unexpected frame type: %v", responseFrame.Type)
	}

	return nil
}

// Stop the client by sending HAProxyDisconnect frame
func (c *Client) Stop() error {
	f := frame.AcquireFrame()
	defer frame.ReleaseFrame(f)
	f.Type = frame.TypeHAProxyDisconnect
	f.StreamID = 0
	f.FrameID = 0
	f.KV.Add("status-code", uint32(0))
	f.KV.Add("message", "normal")

	err := c.send(f)
	if err != nil {
		return err
	}

	responseFrame := frame.AcquireFrame()
	defer frame.ReleaseFrame(responseFrame)
	if err := responseFrame.ReadLimit(c.reader, frame.MaxFrameSize, frame.FromAgent); err != nil {
		return fmt.Errorf("error read AgentDisconnect frame: %w", err)
	}

	if responseFrame.Type != frame.TypeAgentDisconnect {
		return fmt.Errorf("unexpected frame type: %v", responseFrame.Type)
	}

	return nil
}
