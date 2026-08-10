package agent

import (
	"net"
	"time"

	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/worker"
)

// Defaults for the deadlines that are on unless a caller turns them off. Both
// fire only against a peer that has stalled, so a generous value costs nothing
// and their absence pins a goroutine indefinitely.
const (
	DefaultHandshakeTimeout = 10 * time.Second
	DefaultWriteTimeout     = 10 * time.Second
)

// Option configures an Agent. Options are variadic so adding one never breaks
// an existing New call.
type Option func(*Agent)

// WithHandshakeTimeout bounds the HELLO exchange. Zero disables it.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(a *Agent) { a.timeouts.Handshake = d }
}

// WithIdleTimeout closes a connection that has carried no frame for d. Zero,
// the default, disables it: closing idle connections is churn that has to be
// coordinated with HAProxy's own "timeout idle".
func WithIdleTimeout(d time.Duration) Option {
	return func(a *Agent) { a.timeouts.Idle = d }
}

// WithWriteTimeout bounds a single payload write. Zero disables it.
func WithWriteTimeout(d time.Duration) Option {
	return func(a *Agent) { a.timeouts.Write = d }
}

func New(handler func(*request.Request), logger logger.Logger, opts ...Option) *Agent {
	agent := &Agent{
		handler:  handler,
		logger:   logger,
		registry: engine.NewRegistry(),
		timeouts: worker.Timeouts{
			Handshake: DefaultHandshakeTimeout,
			Write:     DefaultWriteTimeout,
		},
	}

	for _, opt := range opts {
		opt(agent)
	}

	return agent
}

type Agent struct {
	handler  func(*request.Request)
	logger   logger.Logger
	registry *engine.Registry
	timeouts worker.Timeouts
}

func (agent *Agent) Serve(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			return err
		}

		c := engine.NewConn(conn)
		c.SetWriteTimeout(agent.timeouts.Write)

		go worker.Handle(c, worker.Config{
			Registry: agent.registry,
			Handler:  agent.handler,
			Logger:   agent.logger,
			Timeouts: agent.timeouts,
		})
	}
}
