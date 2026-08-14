package agent

import (
	"context"
	"errors"
	"net"
	"sync"
	"syscall"
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

// DefaultMaxInFlight is the agent-side counterpart of HAProxy's
// "max-waiting-frames" (SPOE 2.0 section 2.2), which defaults to 20. Matching
// it means a conformant peer never reaches this limit; a peer that raised its
// own, or ignores it, is throttled rather than allowed to spawn a goroutine
// per pipelined NOTIFY.
const DefaultMaxInFlight = 20

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

// WithMaxInFlight bounds how many NOTIFY handlers one connection may run at
// once. Set it at or below HAProxy's own "max-waiting-frames". Zero or less
// disables the limit.
//
// At the limit the connection stops dispatching, which stops it reading, which
// is the backpressure: SPOP has no way to refuse a single NOTIFY, so slowing
// down is the only alternative to hanging up. A NOTIFY parked behind the gate
// keeps burning that stream's share of HAProxy's "timeout processing", so
// setting this well below "max-waiting-frames" is choosing SPOE's on-error
// handling over letting goroutines pile up, and should be a deliberate choice
// rather than a surprise.
//
// A connection parked here stops reading its socket entirely, which has two
// consequences beyond throughput. The peer closing its side is never noticed:
// there is no read in flight to hit EOF, so the connection stays in the
// agent's roster, keeps its WaitGroup count, and -- with WithMaxConnections --
// keeps its connection slot, all until Shutdown tears it down. And
// WithIdleTimeout does not help: the idle deadline only fires against a read
// that is actually armed, and none is armed while a NOTIFY sits here, so the
// timeout an operator would naturally reach for to bound a stuck connection is
// inert in exactly this state.
func WithMaxInFlight(n int) Option {
	return func(a *Agent) { a.maxInFlight = n }
}

// WithMaxConnections bounds how many connections the agent serves at once. At
// the limit it stops accepting until a slot frees. Zero or less, the default,
// disables the limit: legitimate pool sizes vary per deployment, and a default
// guessed low presents as HAProxy being unable to open the pool it is
// configured for.
//
// The limit is shared across every Serve call on this Agent, not per listener.
// Each Serve loop reserves its slot before Accept, so with N as the limit and
// M listeners (IPv4 + IPv6, or TCP + a unix socket, are both normal SPOA
// setups), whichever loops win a permit hold it while parked in Accept, and an
// idle listener that never won one never accepts a connection, even with the
// agent otherwise idle: the effective ceiling is N minus however many
// listeners are sitting on a permit they are not using, and once M exceeds N
// some listeners get none at all. Acquiring after Accept instead would avoid
// that, but at the cost of an accept and a close for every connection this
// agent refuses, which is the trade the design explicitly rejected. An agent
// serving M listeners should set this above M.
func WithMaxConnections(n int) Option {
	return func(a *Agent) { a.maxConnections = n }
}

func New(handler func(context.Context, *request.Request), logger logger.Logger, opts ...Option) *Agent {
	ctx, cancel := context.WithCancel(context.Background())

	agent := &Agent{
		handler: handler,
		logger:  logger,
		timeouts: worker.Timeouts{
			Handshake: DefaultHandshakeTimeout,
			Write:     DefaultWriteTimeout,
		},
		maxInFlight: DefaultMaxInFlight,
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
		conns:       make(map[*engine.Conn]struct{}),
	}

	for _, opt := range opts {
		opt(agent)
	}

	// Built after the options, which are what decide the capacity. A
	// non-positive value must leave this nil rather than produce a
	// zero-capacity channel that would block on the first acquire.
	if agent.maxConnections > 0 {
		agent.connSlots = make(chan struct{}, agent.maxConnections)
	}

	return agent
}

type Agent struct {
	handler  func(context.Context, *request.Request)
	logger   logger.Logger
	timeouts worker.Timeouts

	maxInFlight    int
	maxConnections int

	// connSlots bounds the connections served at once. Nil when unlimited: a
	// send on a nil channel blocks forever, which is the exact opposite of
	// disabled, so every use goes through acquireConn/releaseConn.
	connSlots chan struct{}

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

// Accept backoff bounds, matching net/http.Server.Serve. A temporary accept
// error is usually the descriptor table filling up, and retrying it as fast as
// the CPU allows pins a core on top of the exhaustion that caused it;
// starving the connections already being served of the very goroutine that
// would run them.
const (
	minAcceptBackoff = 5 * time.Millisecond
	maxAcceptBackoff = 1 * time.Second
)

func nextAcceptBackoff(d time.Duration) time.Duration {
	if d == 0 {
		return minAcceptBackoff
	}

	if d *= 2; d > maxAcceptBackoff {
		return maxAcceptBackoff
	}

	return d
}

// transientAcceptErrors are the accept failures a backoff can outlast: the
// listener is intact and the resource it wants will be returned by the
// connections already being served. Everything else ends Serve, because an
// agent that cannot tell whether an error will clear should not retry it
// forever.
//
// This is the list net.Error.Temporary reports, minus the guesswork: that
// method is deprecated precisely because "temporary" was never well defined,
// and it answers false for ENOBUFS and ENOMEM, which are exactly the
// exhaustion this loop exists to ride out.
var transientAcceptErrors = []error{
	syscall.EINTR,
	syscall.EMFILE,
	syscall.ENFILE,
	syscall.ENOBUFS,
	syscall.ENOMEM,
}

// isTransientAcceptError reports whether Accept can be expected to succeed
// again after a pause.
func isTransientAcceptError(err error) bool {
	// Deliberately not net.Error, whose interface still carries the deprecated
	// Temporary method: this asks the one question worth asking, and accepts
	// any error able to answer it.
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return true
	}

	for _, transient := range transientAcceptErrors {
		if errors.Is(err, transient) {
			return true
		}
	}

	return false
}

// pause waits out an accept backoff, reporting false if a drain began first.
// Sleeping outright would make a shutdown sit through up to a second of a delay
// that exists only to throttle a listener nobody is accepting from any more.
func (agent *Agent) pause(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-agent.done:
		return false
	}
}

// acquireConn takes a connection slot, blocking while the agent is at its
// limit. Reports false if a drain began first: a Serve parked here is not in
// Accept, so closing the listener cannot wake it and only the done channel
// can.
func (agent *Agent) acquireConn() bool {
	if agent.connSlots == nil {
		return true
	}

	// Not load-bearing the way worker.acquireSlot's equivalent check is: every
	// path below that follows a lost race already handles a drain correctly.
	// This is a fast path for determinism, matching the other gate, so the two
	// semaphores do not read as having diverged by oversight.
	if agent.isDraining() {
		return false
	}

	select {
	case agent.connSlots <- struct{}{}:
		return true
	case <-agent.done:
		return false
	}
}

func (agent *Agent) releaseConn() {
	if agent.connSlots == nil {
		return
	}

	<-agent.connSlots
}

func (agent *Agent) Serve(listener net.Listener) error {
	if !agent.addListener(listener) {
		return ErrShutdown
	}

	var backoff time.Duration

	for {
		// Before the accept, so a connection this agent will not serve costs it
		// nothing: no accept, no engine.Conn, no worker. Every path below that
		// does not reach the worker goroutine has to release this.
		if !agent.acquireConn() {
			return ErrShutdown
		}

		conn, err := listener.Accept()
		if err != nil {
			agent.releaseConn()

			if agent.isDraining() {
				return ErrShutdown
			}

			if isTransientAcceptError(err) {
				backoff = nextAcceptBackoff(backoff)
				agent.logger.Errorf("accept failed, retrying in %v: %v", backoff, err)

				if !agent.pause(backoff) {
					return ErrShutdown
				}

				continue
			}

			return err
		}

		// The condition cleared, so the next failure starts from the floor
		// again rather than from whatever this one grew to.
		backoff = 0

		c := engine.NewConn(conn)
		c.SetWriteTimeout(agent.timeouts.Write)

		if !agent.track(c) {
			agent.releaseConn()

			// The drain began between Accept and here. This connection has not
			// shaken hands, so there is nothing to say goodbye to.
			if err := c.Close(); err != nil {
				agent.logger.Errorf("close connection: %v", err)
			}

			return ErrShutdown
		}

		go func() {
			defer agent.releaseConn()
			defer agent.forget(c)

			worker.Handle(c, worker.Config{
				Handler:     agent.handler,
				Logger:      agent.logger,
				Timeouts:    agent.timeouts,
				MaxInFlight: agent.maxInFlight,
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
// promise every handler ran to completion; only that none was left running
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
	// already has `done` closed; which is what makes that second check catch
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
