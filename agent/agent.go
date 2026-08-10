package agent

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/worker"
)

// ErrShutdown is what Serve returns once Shutdown has been called, so a caller
// can tell a deliberate stop from a listener that broke.
var ErrShutdown = errors.New("agent: shutdown")

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

func New(handler func(context.Context, *request.Request), logger logger.Logger, opts ...Option) *Agent {
	ctx, cancel := context.WithCancel(context.Background())

	agent := &Agent{
		handler:  handler,
		logger:   logger,
		registry: engine.NewRegistry(),
		timeouts: worker.Timeouts{
			Handshake: DefaultHandshakeTimeout,
			Write:     DefaultWriteTimeout,
		},
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		conns:  make(map[*engine.Conn]struct{}),
	}

	for _, opt := range opts {
		opt(agent)
	}

	return agent
}

type Agent struct {
	handler  func(context.Context, *request.Request)
	logger   logger.Logger
	registry *engine.Registry
	timeouts worker.Timeouts

	// ctx parents every worker's connection context. Cancelling it reaches
	// every handler still running, which is what lets the force close at grace
	// expiry stop work rather than orphan it.
	ctx    context.Context
	cancel context.CancelFunc

	// done is closed when the drain begins. Workers leave their read loops on
	// it.
	done chan struct{}

	mu        sync.Mutex
	draining  bool
	listeners []net.Listener
	conns     map[*engine.Conn]struct{}
	wg        sync.WaitGroup
}

func (agent *Agent) Serve(listener net.Listener) error {
	if !agent.addListener(listener) {
		return ErrShutdown
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			if agent.isDraining() {
				return ErrShutdown
			}

			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}

			return err
		}

		c := engine.NewConn(conn)
		c.SetWriteTimeout(agent.timeouts.Write)

		if !agent.track(c) {
			// The drain began between Accept and here. This connection has not
			// shaken hands, so there is nothing to say goodbye to.
			if err := c.Close(); err != nil {
				agent.logger.Errorf("close connection: %v", err)
			}

			return ErrShutdown
		}

		go func() {
			defer agent.forget(c)

			worker.Handle(c, worker.Config{
				Registry:    agent.registry,
				Handler:     agent.handler,
				Logger:      agent.logger,
				Timeouts:    agent.timeouts,
				Done:        agent.done,
				BaseContext: agent.ctx,
			})
		}()
	}
}

// Shutdown stops accepting, lets in-flight handlers finish and their ACKs go
// out, says goodbye on every connection, and closes. It returns ctx.Err() if
// the grace period expires first, having cancelled the handlers and closed the
// connections still running.
//
// That drain-and-ACK promise only holds for connections that survive to be
// drained. A peer that has already closed its side (common: HAProxy is
// usually being stopped too) makes its worker exit via EOF with nothing left
// to deliver an ACK to, so that worker cancels its still-running handlers
// immediately instead of waiting for them. A nil return therefore does not
// promise every handler ran to completion -- only that none was left running
// past the grace period.
//
// Serve returns ErrShutdown as soon as the listener closes; it does not wait
// for the drain. Calling Shutdown more than once is a no-op after the first.
func (agent *Agent) Shutdown(ctx context.Context) error {
	agent.mu.Lock()
	if agent.draining {
		agent.mu.Unlock()
		return nil
	}

	// Set before anything else, and under the same mutex track takes. That is
	// what keeps wg.Add from racing the wg.Wait below. It also establishes the
	// ordering the deadline poke below depends on: this close happens-before
	// any poke, so worker's armReadDeadline can re-check shuttingDown() right
	// after arming its own deadline and trust that a poke able to land by then
	// already has `done` closed -- which is what makes that second check catch
	// the race instead of missing it.
	agent.draining = true
	close(agent.done)

	listeners := agent.listeners
	agent.listeners = nil

	conns := agent.liveConns()
	agent.mu.Unlock()

	for _, l := range listeners {
		if err := l.Close(); err != nil {
			agent.logger.Errorf("close listener: %v", err)
		}
	}

	// Wake every read loop without touching the socket's writability, so a
	// woken worker can still write the ACKs it owes and its goodbye.
	for _, c := range conns {
		if err := c.SetReadDeadline(time.Now()); err != nil {
			agent.logger.Errorf("wake read loop: %v", err)
		}
	}

	drained := make(chan struct{})
	go func() {
		agent.wg.Wait()
		close(drained)
	}()

	select {
	case <-drained:
		// Every handler has either returned on its own or been cancelled by
		// its worker (a connection the peer already closed cancels its
		// handlers immediately rather than waiting for them), so this only
		// releases the context's own resources.
		agent.cancel()

		return nil

	case <-ctx.Done():
		// Cancel before closing, so a handler that watches its context gets the
		// signal a moment ahead of its socket dying.
		agent.cancel()
		agent.closeAll()

		return ctx.Err()
	}
}

// liveConns snapshots the roster. Callers hold agent.mu.
func (agent *Agent) liveConns() []*engine.Conn {
	conns := make([]*engine.Conn, 0, len(agent.conns))
	for c := range agent.conns {
		conns = append(conns, c)
	}

	return conns
}

// closeAll tears down whatever survived the grace period.
func (agent *Agent) closeAll() {
	agent.mu.Lock()
	conns := agent.liveConns()
	agent.mu.Unlock()

	for _, c := range conns {
		if err := c.Close(); err != nil {
			agent.logger.Errorf("close connection: %v", err)
		}
	}
}

// track registers a connection so Shutdown can reach it, reporting false once
// the drain has begun.
func (agent *Agent) track(c *engine.Conn) bool {
	agent.mu.Lock()
	defer agent.mu.Unlock()

	if agent.draining {
		return false
	}

	agent.conns[c] = struct{}{}
	agent.wg.Add(1)

	return true
}

// forget deregisters a connection whose worker has finished, so the roster does
// not grow with connections the peer closed long ago.
func (agent *Agent) forget(c *engine.Conn) {
	agent.mu.Lock()
	delete(agent.conns, c)
	agent.mu.Unlock()

	agent.wg.Done()
}

func (agent *Agent) addListener(l net.Listener) bool {
	agent.mu.Lock()
	defer agent.mu.Unlock()

	if agent.draining {
		return false
	}

	agent.listeners = append(agent.listeners, l)

	return true
}

func (agent *Agent) isDraining() bool {
	agent.mu.Lock()
	defer agent.mu.Unlock()

	return agent.draining
}
