package client

import (
	"net"
	"testing"
	"time"

	"github.com/go-spop/spop/frame"
)

func TestClientInitAcceptsAnAgentHello(t *testing.T) {
	c, agent := newTestClient(t)

	replied := make(chan struct{})

	go func() {
		defer close(replied)

		read(t, agent)

		reply := frame.NewFrame()
		reply.Type = frame.TypeAgentHello
		reply.KV.Add("version", "2.0")
		reply.KV.Add("max-frame-size", uint32(16384))
		reply.KV.Add("capabilities", "pipelining")

		if _, err := reply.Encode(agent); err != nil {
			t.Errorf("writing the AGENT-HELLO: %v", err)
		}
	}()

	if err := c.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	<-replied
}

func TestClientInitReportsAnUnreadableReply(t *testing.T) {
	c, agent := newTestClient(t)

	go func() {
		read(t, agent)

		if _, err := agent.Write([]byte{0x00, 0x00, 0x00, 0x06, 0x65, 0x00, 0x00, 0x00, 0x01, 0xff}); err != nil {
			t.Errorf("writing the reply: %v", err)
		}
	}()

	if err := c.Init(); err == nil {
		t.Fatal("expected an error for an unreadable AGENT-HELLO, got nil")
	}
}

func TestClientNotifyReportsAnUnreadableReply(t *testing.T) {
	c, agent := newTestClient(t)

	go func() {
		read(t, agent)

		if _, err := agent.Write([]byte{0x00, 0x00, 0x00, 0x08, 0x67, 0x00, 0x00, 0x00, 0x01, 0x07, 0x09, 0x7f}); err != nil {
			t.Errorf("writing the reply: %v", err)
		}
	}()

	if err := c.Notify(); err == nil {
		t.Fatal("expected an error for an unreadable AGENT-ACK, got nil")
	}
}

func newTestClient(t *testing.T) (Client, net.Conn) {
	t.Helper()

	clientConn, agent := net.Pipe()

	t.Cleanup(func() {
		clientConn.Close()
		agent.Close()
	})

	if err := clientConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the client deadline: %v", err)
	}

	if err := agent.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the agent deadline: %v", err)
	}

	return NewClient(clientConn), agent
}

func read(t *testing.T, conn net.Conn) {
	t.Helper()

	f := frame.NewFrame()
	if err := f.Read(conn); err != nil {
		t.Errorf("reading the client's frame: %v", err)
	}
}
