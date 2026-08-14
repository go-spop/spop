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

// tempError is the shape net.Error.Temporary() classifies as retryable. The
// real trigger is EMFILE/ENFILE; the descriptor table filling under exactly
// the connection burst this agent exists to serve.
type tempError struct{}

func (tempError) Error() string   { return "temporary accept failure" }
func (tempError) Timeout() bool   { return false }
func (tempError) Temporary() bool { return true }

// fakeListener hands Serve a scripted sequence of Accept results and counts the
// calls, which is how a test can see a busy-loop that a wall-clock assertion
// could only guess at.
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

// A persistent temporary error must not spin the accept loop. Without a
// backoff this loop runs as fast as the CPU allows; millions of calls in the
// window below; so a generous ceiling separates "backing off" from "spinning"
// without asserting any particular timing.
func TestAgent_ServeBacksOffOnATemporaryAcceptError(t *testing.T) {
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

	// Exponential backoff from 5ms capped at 1s reaches roughly a dozen calls
	// in this window. An unthrottled loop reaches six figures or more.
	if calls := l.calls.Load(); calls > 100 {
		t.Fatalf("accept was retried %d times; the loop is not backing off", calls)
	}
}

// A temporary error must throttle the loop without breaking it: connections
// arriving between failures are still served.
//
// This deliberately does NOT assert that the delay resets after a success.
// Proving a reset means measuring that the next retry came sooner than the
// previous delay, which is a wall-clock assertion of exactly the kind this
// repo has twice been flaked by. The reset is implemented because net/http
// does it and the alternative; a delay that only ever grows; would leave a
// recovered listener throttled forever; it is left unasserted rather than
// asserted badly.
func TestAgent_ServeKeepsAcceptingBetweenTemporaryErrors(t *testing.T) {
	accepted := make(chan struct{}, 2)

	l := &fakeListener{}
	l.accept = func(calls int64) (net.Conn, error) {
		switch calls {
		case 1, 3:
			return nil, tempError{}

		case 2, 4:
			client, server := net.Pipe()

			// Close the peer end at once. The worker reads EOF and exits, so
			// this test never has an unread connection for the drain to block
			// writing its goodbye to.
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

func TestAgent_ServeReleasesTheConnectionSlotOnAnAcceptError(t *testing.T) {
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

func TestAgent_ServeReturnsAPermanentAcceptError(t *testing.T) {
	want := errors.New("listener is broken")

	l := &fakeListener{accept: func(int64) (net.Conn, error) { return nil, want }}

	a := New(noopHandler, logger.NewNop())

	if err := a.Serve(l); !errors.Is(err, want) {
		t.Fatalf("expected the permanent error, got %v", err)
	}
}

func TestAgent_ShutdownInterruptsAnAcceptBackoff(t *testing.T) {
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
