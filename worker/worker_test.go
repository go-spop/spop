package worker

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-spop/spop/client"
	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
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
		Handle(engine.NewConn(server), Config{
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
		Handle(engine.NewConn(server), Config{
			Handler: m.Handle,
			Logger:  logger.NewNop(),
		})
	}()
	go func() {
		Handle(engine.NewConn(server2), Config{
			Handler: m.Handle,
			Logger:  logger.NewNop(),
		})
	}()
	duration := time.Second
	loop := func(s client.Client) {
		if s.Init() != nil {
			t.Fatal("unexpected error on Init")
		}
		for {
			select {
			case <-time.After(duration):
				s.Stop()
			default:
				s.Notify()
			}
		}
	}
	go loop(spoe)
	go loop(spoe2)

	<-time.After(duration)
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
		Handle(engine.NewConn(server), Config{
			Handler: m.Handle,
			Logger:  logger.NewNop(),
		})
		m.Finish()
	}()

	spoe.Init()
	for n := 0; n < b.N; n++ {
		spoe.Notify()
	}
	spoe.Stop()

	<-time.After(time.Millisecond * 100)
	clientConn.Close()
}
