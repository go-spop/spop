package client

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/internal/spec"
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
func (c *Client) Init() (err error) {
	f := frame.AcquireFrame()

	defer frame.ReleaseFrame(f)

	f.Type = frame.TypeHAProxyHello
	f.StreamID = 0
	f.FrameID = 0

	f.KV.Add("supported-versions", strings.Join(spec.SupportedVersions, ", "))
	f.KV.Add("max-frame-size", uint32(16*1024))
	f.KV.Add("capabilities", "")

	if err = c.send(f); err != nil {
		return err
	}

	responseFrame := frame.AcquireFrame()

	defer frame.ReleaseFrame(responseFrame)

	if err = responseFrame.Read(c.reader); err != nil {
		return fmt.Errorf("error reading AgentHello: %w", err)
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
	n, err := f.Encode(buf)
	if err != nil {
		return err
	}
	n, err = c.conn.Write(buf.Bytes())
	if err != nil {
		return err
	}
	if n != buf.Len() {
		return fmt.Errorf("size mismatch")
	}
	return nil
}

// Notify send an empty Notify frame
func (c *Client) Notify() (err error) {
	f := frame.AcquireFrame()

	defer frame.ReleaseFrame(f)

	f.Type = frame.TypeNotify
	f.StreamID = 1
	f.FrameID = 1

	if err = c.send(f); err != nil {
		return err
	}

	responseFrame := frame.AcquireFrame()

	defer frame.ReleaseFrame(responseFrame)

	if err = responseFrame.Read(c.reader); err != nil {
		return fmt.Errorf("error reading AgentAck: %w", err)
	}

	return nil
}

// Stop the client by sending HAProxyDisconnect frame
func (c *Client) Stop() (err error) {
	f := frame.AcquireFrame()

	defer frame.ReleaseFrame(f)

	f.Type = frame.TypeHAProxyDisconnect
	f.StreamID = 0
	f.FrameID = 0

	f.KV.Add("status-code", uint32(0))
	f.KV.Add("message", "normal")

	if err = c.send(f); err != nil {
		return err
	}

	responseFrame := frame.AcquireFrame()

	defer frame.ReleaseFrame(responseFrame)

	if err = responseFrame.Read(c.reader); err != nil {
		return fmt.Errorf("error reading AgentDisconnect: %w", err)
	}

	return nil
}
