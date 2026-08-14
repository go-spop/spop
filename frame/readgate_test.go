package frame

import (
	"bytes"
	"testing"

	"github.com/go-spop/spop/action"
)

func TestReadLimitAcceptsOnlyTheTypesItWasGiven(t *testing.T) {
	tests := []struct {
		name   string
		accept []Type
		build  func(*Frame)
		wantOK bool
	}{
		{
			name:   "an agent accepts a NOTIFY",
			accept: FromHAProxy,
			build:  func(f *Frame) { f.Type = TypeNotify },
			wantOK: true,
		},
		{
			name:   "an agent accepts a HAPROXY-HELLO",
			accept: FromHAProxy,
			build: func(f *Frame) {
				f.Type = TypeHAProxyHello
				f.KV.Add("supported-versions", "2.0")
			},
			wantOK: true,
		},
		{
			name:   "an agent refuses an AGENT-ACK",
			accept: FromHAProxy,
			build:  func(f *Frame) { f.Type = TypeAgentAck },
		},
		{
			name:   "an agent refuses an AGENT-HELLO",
			accept: FromHAProxy,
			build:  func(f *Frame) { f.Type = TypeAgentHello },
		},
		{
			name:   "a client accepts an AGENT-ACK",
			accept: FromAgent,
			build:  func(f *Frame) { f.Type = TypeAgentAck },
			wantOK: true,
		},
		{
			name:   "a client refuses a NOTIFY",
			accept: FromAgent,
			build:  func(f *Frame) { f.Type = TypeNotify },
		},
		{
			name:   "no set accepts either direction",
			accept: nil,
			build:  func(f *Frame) { f.Type = TypeAgentAck },
			wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFrame()
			tc.build(f)

			buf := &bytes.Buffer{}
			if _, err := f.Encode(buf); err != nil {
				t.Fatalf("encoding: %v", err)
			}

			got := NewFrame()
			err := got.ReadLimit(bytes.NewReader(buf.Bytes()), MaxFrameSize, tc.accept)

			if tc.wantOK && err != nil {
				t.Fatalf("expected the frame to be accepted, got %v", err)
			}

			if !tc.wantOK && err == nil {
				t.Fatalf("expected frame type %d to be refused, got nil", f.Type)
			}
		})
	}
}

func TestReadLimitRefusesBeforeReadingTheBody(t *testing.T) {
	f := NewFrame()
	f.Type = TypeAgentAck
	f.Actions = action.Actions{action.NewSetVar(action.ScopeSession, "ip_score", uint32(42))}

	buf := &bytes.Buffer{}
	if _, err := f.Encode(buf); err != nil {
		t.Fatalf("encoding: %v", err)
	}

	encoded := buf.Bytes()
	src := bytes.NewReader(encoded)

	got := NewFrame()
	if err := got.ReadLimit(src, MaxFrameSize, FromHAProxy); err == nil {
		t.Fatal("expected the frame to be refused, got nil")
	}

	if src.Len() != len(encoded)-readHeaderBytes {
		t.Fatalf("expected only the %d header bytes to be consumed, %d of %d remain", readHeaderBytes, src.Len(), len(encoded))
	}
}

const readHeaderBytes = 5
