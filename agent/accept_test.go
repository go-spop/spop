package agent

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-spop/spop/logger"
)

func TestAgentServeBacksOffOnATemporaryAcceptError(t *testing.T) {
	l := &fakeListener{accept: func(int64) (net.Conn, error) { return nil, tempError{} }}

	a := New(noopHandler, logger.NewNop())

	served := make(chan error, 1)
	go func() { served <- a.Serve(l) }()

	time.Sleep(250 * time.Millisecond)

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	select {
	case err := <-served:
		if !errors.Is(err, ErrShutdown) {
			t.Fatalf("expected ErrShutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve never returned")
	}

	if calls := l.calls.Load(); calls > 100 {
		t.Fatalf("accept was retried %d times; the loop is not backing off", calls)
	}
}

func TestAgentServeKeepsAcceptingBetweenTemporaryErrors(t *testing.T) {
	accepted := make(chan struct{}, 2)

	l := &fakeListener{}
	l.accept = func(calls int64) (net.Conn, error) {
		switch calls {
		case 1, 3:
			return nil, tempError{}

		case 2, 4:
			client, server := net.Pipe()

			client.Close()
			accepted <- struct{}{}

			return server, nil
		}

		return nil, net.ErrClosed
	}

	a := New(noopHandler, logger.NewNop())

	served := make(chan error, 1)
	go func() { served <- a.Serve(l) }()

	for i := range 2 {
		select {
		case <-accepted:
		case <-time.After(5 * time.Second):
			t.Fatalf("accept %d never succeeded", i+1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve never returned")
	}
}

func TestAgentServeReleasesTheConnectionSlotOnAnAcceptError(t *testing.T) {
	retried := make(chan struct{}, 1)
	l := &fakeListener{accept: func(calls int64) (net.Conn, error) {
		if calls == 2 {
			select {
			case retried <- struct{}{}:
			default:
			}
		}

		return nil, tempError{}
	}}

	a := New(noopHandler, logger.NewNop(), WithMaxConnections(1))

	served := make(chan error, 1)
	go func() { served <- a.Serve(l) }()

	select {
	case <-retried:
	case <-time.After(5 * time.Second):
		t.Fatal("Accept was never retried; the slot was not released on error")
	}

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	select {
	case err := <-served:
		if !errors.Is(err, ErrShutdown) {
			t.Fatalf("expected ErrShutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve never returned")
	}
}

func TestAgentServeReturnsAPermanentAcceptError(t *testing.T) {
	want := errors.New("listener is broken")

	l := &fakeListener{accept: func(int64) (net.Conn, error) { return nil, want }}

	a := New(noopHandler, logger.NewNop())

	if err := a.Serve(l); !errors.Is(err, want) {
		t.Fatalf("expected the permanent error, got %v", err)
	}
}

func TestAgentShutdownInterruptsAnAcceptBackoff(t *testing.T) {
	l := &fakeListener{accept: func(int64) (net.Conn, error) { return nil, tempError{} }}

	a := New(noopHandler, logger.NewNop())

	served := make(chan error, 1)
	go func() { served <- a.Serve(l) }()

	time.Sleep(300 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("expected a clean shutdown, got %v", err)
	}

	select {
	case err := <-served:
		if !errors.Is(err, ErrShutdown) {
			t.Fatalf("expected ErrShutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return; the backoff outlasted the drain")
	}
}

type tempError struct{}

func (tempError) Error() string { return "temporary accept failure" }

func (tempError) Timeout() bool { return false }

func (tempError) Temporary() bool { return true }

type fakeListener struct {
	mu     sync.Mutex
	accept func(calls int64) (net.Conn, error)

	calls  atomic.Int64
	closed atomic.Bool
}

func (l *fakeListener) Accept() (net.Conn, error) {
	n := l.calls.Add(1)

	if l.closed.Load() {
		return nil, net.ErrClosed
	}

	l.mu.Lock()
	fn := l.accept
	l.mu.Unlock()

	return fn(n)
}

func (l *fakeListener) Close() error {
	l.closed.Store(true)
	return nil
}

func (l *fakeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}
