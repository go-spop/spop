package agent

import (
	"context"
	"testing"
	"time"

	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

func noopHandler(context.Context, *request.Request) {}

func TestNewDefaults(t *testing.T) {
	a := New(noopHandler, logger.NewNop())

	if a.timeouts.Handshake != DefaultHandshakeTimeout {
		t.Fatalf("expected the handshake default %v, got %v", DefaultHandshakeTimeout, a.timeouts.Handshake)
	}

	if a.timeouts.Write != DefaultWriteTimeout {
		t.Fatalf("expected the write default %v, got %v", DefaultWriteTimeout, a.timeouts.Write)
	}

	if a.timeouts.Idle != 0 {
		t.Fatalf("expected idle to default to disabled, got %v", a.timeouts.Idle)
	}
}

func TestNewOptions(t *testing.T) {
	a := New(noopHandler, logger.NewNop(),
		WithHandshakeTimeout(time.Second),
		WithIdleTimeout(2*time.Second),
		WithWriteTimeout(3*time.Second),
	)

	if a.timeouts.Handshake != time.Second {
		t.Fatalf("expected 1s, got %v", a.timeouts.Handshake)
	}

	if a.timeouts.Idle != 2*time.Second {
		t.Fatalf("expected 2s, got %v", a.timeouts.Idle)
	}

	if a.timeouts.Write != 3*time.Second {
		t.Fatalf("expected 3s, got %v", a.timeouts.Write)
	}
}

func TestNewOptionsCanDisableADefault(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithHandshakeTimeout(0), WithWriteTimeout(0))

	if a.timeouts.Handshake != 0 {
		t.Fatalf("expected the handshake timeout to be disabled, got %v", a.timeouts.Handshake)
	}

	if a.timeouts.Write != 0 {
		t.Fatalf("expected the write timeout to be disabled, got %v", a.timeouts.Write)
	}
}

func TestNewLastOptionWins(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithIdleTimeout(time.Second), WithIdleTimeout(5*time.Second))

	if a.timeouts.Idle != 5*time.Second {
		t.Fatalf("expected 5s, got %v", a.timeouts.Idle)
	}
}

func TestNewMaxInFlightDefault(t *testing.T) {
	a := New(noopHandler, logger.NewNop())

	if a.maxInFlight != DefaultMaxInFlight {
		t.Fatalf("expected the in-flight default %d, got %d", DefaultMaxInFlight, a.maxInFlight)
	}
}

func TestNewMaxInFlightOption(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithMaxInFlight(5))

	if a.maxInFlight != 5 {
		t.Fatalf("expected 5, got %d", a.maxInFlight)
	}
}

func TestNewMaxInFlightCanBeDisabled(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithMaxInFlight(0))

	if a.maxInFlight != 0 {
		t.Fatalf("expected the in-flight limit to be disabled, got %d", a.maxInFlight)
	}
}

func TestNewMaxConnectionsDefaultsToUnlimited(t *testing.T) {
	a := New(noopHandler, logger.NewNop())

	if a.maxConnections != 0 {
		t.Fatalf("expected connections to default to unlimited, got %d", a.maxConnections)
	}

	if a.connSlots != nil {
		t.Fatal("expected no connection semaphore when the limit is disabled")
	}
}

func TestNewMaxConnectionsOption(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithMaxConnections(3))

	if a.maxConnections != 3 {
		t.Fatalf("expected 3, got %d", a.maxConnections)
	}

	if cap(a.connSlots) != 3 {
		t.Fatalf("expected a semaphore of capacity 3, got %d", cap(a.connSlots))
	}
}
