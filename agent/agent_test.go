package agent

import (
	"context"
	"testing"
	"time"

	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

func noopHandler(context.Context, *request.Request) {}

// The handshake and write deadlines are on by default: they fire only against a
// pathological peer, and without them a stalled socket pins a goroutine
// forever. Idle is opt-in, because closing idle connections is churn an
// operator has to coordinate with HAProxy's own "timeout idle" -- and under
// async it also removes failover partners from an engine.
func TestNew_defaults(t *testing.T) {
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

func TestNew_options(t *testing.T) {
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

// Zero must reach the worker as zero, so an integrator can turn a default off.
func TestNew_optionsCanDisableADefault(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithHandshakeTimeout(0), WithWriteTimeout(0))

	if a.timeouts.Handshake != 0 {
		t.Fatalf("expected the handshake timeout to be disabled, got %v", a.timeouts.Handshake)
	}

	if a.timeouts.Write != 0 {
		t.Fatalf("expected the write timeout to be disabled, got %v", a.timeouts.Write)
	}
}

// Later options win, so a caller building a slice can override.
func TestNew_lastOptionWins(t *testing.T) {
	a := New(noopHandler, logger.NewNop(), WithIdleTimeout(time.Second), WithIdleTimeout(5*time.Second))

	if a.timeouts.Idle != 5*time.Second {
		t.Fatalf("expected 5s, got %v", a.timeouts.Idle)
	}
}
