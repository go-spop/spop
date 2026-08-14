package worker

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-spop/spop/client"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/transport"
)

func TestWorker(t *testing.T) {
	clientConn, server := net.Pipe()
	spoe := client.NewClient(clientConn)
	m := MockedHandler{
		handleFunc: func(_ context.Context, r *request.Request) {

		},
		finishFunc: func() {

		},
	}

	go func() {
		Handle(transport.NewConn(server), Config{
			Handler: m.Handle,
			Logger:  logger.NewNop(),
		})
		m.Finish()
	}()
	if spoe.Init() != nil {
		t.Fatal("unexpected error on Init")
	}
	if spoe.Notify() != nil {
		t.Fatal("unexpected error on Notify")
	}
	if spoe.Stop() != nil {
		t.Fatal("unexpected error on Stop")
	}

	<-time.After(time.Millisecond * 100)
	clientConn.Close()
}

func TestWorkerConcurrent(t *testing.T) {
	clientConn, server := net.Pipe()
	clientConn2, server2 := net.Pipe()
	spoe := client.NewClient(clientConn)
	spoe2 := client.NewClient(clientConn2)
	m := MockedHandler{
		handleFunc: func(_ context.Context, r *request.Request) {

		},
		finishFunc: func() {

		},
	}

	go func() {
		Handle(transport.NewConn(server), Config{
			Handler: m.Handle,
			Logger:  logger.NewNop(),
		})
	}()
	go func() {
		Handle(transport.NewConn(server2), Config{
			Handler: m.Handle,
			Logger:  logger.NewNop(),
		})
	}()
	stop := make(chan struct{})

	var wg sync.WaitGroup

	loop := func(s client.Client) {
		defer wg.Done()

		if err := s.Init(); err != nil {
			t.Errorf("unexpected error on Init: %v", err)
			return
		}

		for {
			select {
			case <-stop:
				if err := s.Stop(); err != nil {
					t.Errorf("unexpected error on Stop: %v", err)
				}

				return
			default:
			}

			if err := s.Notify(); err != nil {
				t.Errorf("unexpected error on Notify: %v", err)
				return
			}
		}
	}

	wg.Add(2)

	go loop(spoe)
	go loop(spoe2)

	<-time.After(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

type MockedHandler struct {
	handleFunc func(ctx context.Context, r *request.Request)
	finishFunc func()
}

func (h *MockedHandler) Handle(ctx context.Context, r *request.Request) {
	h.handleFunc(ctx, r)
}

func (h *MockedHandler) Finish() {
	h.finishFunc()
}

func BenchmarkWorker(b *testing.B) {
	clientConn, server := net.Pipe()
	spoe := client.NewClient(clientConn)
	m := MockedHandler{
		handleFunc: func(_ context.Context, r *request.Request) {

		},
		finishFunc: func() {

		},
	}

	go func() {
		Handle(transport.NewConn(server), Config{
			Handler: m.Handle,
			Logger:  logger.NewNop(),
		})
		m.Finish()
	}()

	if err := spoe.Init(); err != nil {
		b.Fatalf("unexpected error on Init: %v", err)
	}

	for b.Loop() {
		if err := spoe.Notify(); err != nil {
			b.Fatalf("unexpected error on Notify: %v", err)
		}
	}

	if err := spoe.Stop(); err != nil {
		b.Fatalf("unexpected error on Stop: %v", err)
	}

	<-time.After(time.Millisecond * 100)
	clientConn.Close()
}
