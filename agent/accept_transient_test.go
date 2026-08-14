package agent

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/go-spop/spop/logger"
)

func TestAgentServeRetriesTransientAcceptErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"too many open files", syscall.EMFILE},
		{"the system file table is full", syscall.ENFILE},
		{"interrupted", syscall.EINTR},
		{"no buffer space", syscall.ENOBUFS},
		{"out of memory", syscall.ENOMEM},
		{"a timeout", timeoutError{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err
			if _, ok := err.(net.Error); !ok {
				err = &net.OpError{Op: "accept", Net: "tcp", Err: err}
			}

			retried := make(chan struct{}, 1)
			l := &fakeListener{accept: func(calls int64) (net.Conn, error) {
				if calls == 3 {
					select {
					case retried <- struct{}{}:
					default:
					}
				}

				return nil, err
			}}

			a := New(noopHandler, logger.NewNop())

			served := make(chan error, 1)
			go func() { served <- a.Serve(l) }()

			select {
			case <-retried:
			case err := <-served:
				t.Fatalf("Serve gave up on a transient error instead of retrying: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("accept was never retried")
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
		})
	}
}

func TestAgentServeReturnsAnErrorItCannotClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"a plain error", errors.New("listener is broken")},
		{"the listener is closed", net.ErrClosed},
		{"a permission error", &net.OpError{Op: "accept", Err: syscall.EACCES}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := &fakeListener{accept: func(int64) (net.Conn, error) { return nil, tc.err }}

			a := New(noopHandler, logger.NewNop())

			if err := a.Serve(l); !errors.Is(err, tc.err) {
				t.Fatalf("expected %v, got %v", tc.err, err)
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "accept timed out" }

func (timeoutError) Timeout() bool { return true }
