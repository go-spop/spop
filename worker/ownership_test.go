package worker

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

func TestWorkerDispatchReportsFrameOwnership(t *testing.T) {
	tests := []struct {
		name  string
		ready bool
		build func(*testing.T, *frame.Frame)

		wantTransferred bool
		wantDone        bool
		wantErr         bool
	}{
		{
			name:  "notify keeps the frame for its goroutine",
			ready: true,
			build: func(_ *testing.T, f *frame.Frame) { f.Type = frame.TypeNotify },

			wantTransferred: true,
		},
		{
			name:  "notify before the handshake is rejected and keeps nothing",
			ready: false,
			build: func(_ *testing.T, f *frame.Frame) { f.Type = frame.TypeNotify },

			wantErr: true,
		},
		{
			name:  "a completed handshake keeps nothing",
			ready: false,
			build: func(_ *testing.T, f *frame.Frame) {
				f.Type = frame.TypeHAProxyHello
				f.KV.Add("supported-versions", "2.0")
				f.KV.Add("max-frame-size", uint32(16384))
				f.KV.Add("capabilities", "pipelining")
			},
		},
		{
			name:  "a second hello is rejected and keeps nothing",
			ready: true,
			build: func(_ *testing.T, f *frame.Frame) { f.Type = frame.TypeHAProxyHello },

			wantErr: true,
		},
		{
			name:  "disconnect before the handshake is rejected and keeps nothing",
			ready: false,
			build: func(_ *testing.T, f *frame.Frame) { f.Type = frame.TypeHAProxyDisconnect },

			wantErr: true,
		},
		{
			name:  "an unknown frame type is skipped and keeps nothing",
			ready: true,
			build: func(_ *testing.T, f *frame.Frame) { f.Type = frame.Type(0x7f) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorker(t)
			w.ready = tc.ready

			f := frame.AcquireFrame()
			tc.build(t, f)

			transferred, done, err := w.dispatch(f)

			if transferred {

				w.inflight.Wait()
			} else {
				frame.ReleaseFrame(f)
			}

			if transferred != tc.wantTransferred {
				t.Fatalf("expected transferred %v, got %v", tc.wantTransferred, transferred)
			}

			if done != tc.wantDone {
				t.Fatalf("expected done %v, got %v", tc.wantDone, done)
			}

			if (err != nil) != tc.wantErr {
				t.Fatalf("expected an error: %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func newTestWorker(t *testing.T) *worker {
	t.Helper()

	client, server := net.Pipe()
	t.Cleanup(func() { client.Close() })

	go func() { _, _ = io.Copy(io.Discard, client) }()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return &worker{
		conn:    engine.NewConn(server),
		handler: func(context.Context, *request.Request) {},
		logger:  logger.NewNop(),
		ctx:     ctx,
		cancel:  cancel,
	}
}
