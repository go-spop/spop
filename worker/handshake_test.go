package worker

import (
	"bufio"
	"testing"

	"github.com/go-spop/spop/frame"
)

func TestWorkerHandshakeRejectsIncompleteHello(t *testing.T) {
	tests := []struct {
		name     string
		items    []kvItem
		wantCode uint32
	}{
		{
			name: "no supported-versions",
			items: []kvItem{
				{"max-frame-size", uint32(16384)},
				{"capabilities", "pipelining"},
			},
			wantCode: wantStatusCodeNoVersion,
		},
		{
			name: "no max-frame-size",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"capabilities", "pipelining"},
			},
			wantCode: wantStatusCodeNoMaxFrameSize,
		},
		{
			name: "no capabilities",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"max-frame-size", uint32(16384)},
			},
			wantCode: wantStatusCodeNoCapabilities,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertAgentDisconnect(t, exchangeHello(t, helloWith(t, tc.items...)), tc.wantCode)
		})
	}
}

func TestWorkerHandshakeRejectsMistypedItems(t *testing.T) {
	tests := []struct {
		name     string
		items    []kvItem
		wantCode uint32
	}{
		{
			name: "supported-versions is not a string",
			items: []kvItem{
				{"supported-versions", uint32(2)},
				{"max-frame-size", uint32(16384)},
				{"capabilities", "pipelining"},
			},
			wantCode: wantStatusCodeNoVersion,
		},
		{
			name: "capabilities is not a string",
			items: []kvItem{
				{"supported-versions", "2.0"},
				{"max-frame-size", uint32(16384)},
				{"capabilities", uint32(1)},
			},
			wantCode: wantStatusCodeNoCapabilities,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertAgentDisconnect(t, exchangeHello(t, helloWith(t, tc.items...)), tc.wantCode)
		})
	}
}

func TestWorkerHandshakeAcceptsEmptyCapabilities(t *testing.T) {
	hello := helloWith(t,
		kvItem{"supported-versions", "2.0"},
		kvItem{"max-frame-size", uint32(16384)},
		kvItem{"capabilities", ""},
	)

	if got := exchangeHello(t, hello); got.frameType != frame.TypeAgentHello {
		t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
	}
}

func TestWorkerHandshakeRejectsUnsupportedVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions string
	}{

		{"only an older major", "1.5"},
		{"several older majors", "1.5, 1.0"},
		{"not a version at all", "banana"},
		{"empty list", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hello := helloWith(t,
				kvItem{"supported-versions", tc.versions},
				kvItem{"max-frame-size", uint32(16384)},
				kvItem{"capabilities", "pipelining"},
			)

			assertAgentDisconnect(t, exchangeHello(t, hello), wantStatusCodeBadVersion)
		})
	}
}

func TestWorkerHandshakeRejectsFrameSizeBelowTheFloor(t *testing.T) {

	hello := helloWith(t,
		kvItem{"supported-versions", "2.0"},
		kvItem{"max-frame-size", uint32(255)},
		kvItem{"capabilities", "pipelining"},
	)

	assertAgentDisconnect(t, exchangeHello(t, hello), wantStatusCodeBadFrameSize)
}

func TestWorkerHandshakeAcceptsAnnouncedVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions string
	}{
		{"exactly the agent's version", "2.0"},
		{"a list including it", "2.0, 1.5"},
		{"spaces which must be ignored", "  2.0 ,  1.5  "},
		{"a newer major", "3.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hello := helloWith(t,
				kvItem{"supported-versions", tc.versions},
				kvItem{"max-frame-size", uint32(16384)},
				kvItem{"capabilities", "pipelining"},
			)

			got := exchangeHello(t, hello)
			if got.frameType != frame.TypeAgentHello {
				t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
			}

			if v, ok := got.kv.Get("version"); !ok || v != "2.0" {
				t.Fatalf("expected version \"2.0\", got %v", v)
			}
		})
	}
}

func TestWorkerHandshakeAnnouncesTheLowerFrameSize(t *testing.T) {
	tests := []struct {
		name    string
		haproxy uint32
		want    uint32
	}{
		{"HAProxy below the agent's maximum", 16384, 16384},
		{"HAProxy above the agent's maximum", frame.MaxFrameSize * 4, frame.MaxFrameSize},
		{"exactly the agent's maximum", frame.MaxFrameSize, frame.MaxFrameSize},
		{"exactly the floor", 256, 256},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hello := helloWith(t,
				kvItem{"supported-versions", "2.0"},
				kvItem{"max-frame-size", tc.haproxy},
				kvItem{"capabilities", "pipelining"},
			)

			got := exchangeHello(t, hello)
			if got.frameType != frame.TypeAgentHello {
				t.Fatalf("expected an AGENT-HELLO, got frame type %d", got.frameType)
			}

			size, ok := got.kv.Get("max-frame-size")
			if !ok {
				t.Fatal("the AGENT-HELLO carried no max-frame-size")
			}

			if size != tc.want {
				t.Fatalf("expected max-frame-size %d, got %v", tc.want, size)
			}
		})
	}
}

const (
	wantStatusCodeNoVersion      = uint32(5)
	wantStatusCodeNoMaxFrameSize = uint32(6)
	wantStatusCodeNoCapabilities = uint32(7)
	wantStatusCodeBadVersion     = uint32(8)
	wantStatusCodeBadFrameSize   = uint32(9)
)

func helloWith(t *testing.T, items ...kvItem) *frame.Frame {
	t.Helper()

	f := frame.AcquireFrame()
	f.Type = frame.TypeHAProxyHello

	for _, item := range items {
		f.KV.Add(item.name, item.value)
	}

	return f
}

type kvItem struct {
	name  string
	value any
}

func exchangeHello(t *testing.T, hello *frame.Frame) agentFrame {
	t.Helper()

	defer frame.ReleaseFrame(hello)

	conn := startWorker(t)

	if _, err := hello.Encode(conn); err != nil {
		t.Fatalf("writing the HELLO: %v", err)
	}

	return readAgentFrame(t, bufio.NewReader(conn))
}
