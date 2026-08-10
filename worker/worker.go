package worker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

const (
	// Section 3.2.1's capability tokens.
	capabilityPipelining = "pipelining"
	capabilityAsync      = "async"

	// frameLengthPrefix is the 4-byte FRAME-LENGTH field, which the length it
	// declares does not itself count.
	frameLengthPrefix = 4
)

// Status codes from SPOE 2.0 section 3.5.
const (
	statusCodeNormal         uint32 = 0
	statusCodeTimeout        uint32 = 2
	statusCodeFrameTooBig    uint32 = 3
	statusCodeInvalidFrame   uint32 = 4
	statusCodeNoVersion      uint32 = 5
	statusCodeNoMaxFrameSize uint32 = 6
	statusCodeNoCapabilities uint32 = 7
	statusCodeBadVersion     uint32 = 8
	statusCodeBadFrameSize   uint32 = 9
)

// Timeouts bound how long a connection may block. Zero disables a timeout,
// matching net.Conn deadline semantics.
//
// Section 2.2 defines "timeout hello" and "timeout idle" as HAProxy-side
// configuration naming two agent actions: emitting the AGENT-HELLO, and closing
// an idle connection. Neither imposes an agent-side deadline, so these values
// are the library's own policy rather than a conformance requirement.
type Timeouts struct {
	// Handshake bounds the HELLO exchange, from accept until the handshake
	// completes.
	Handshake time.Duration

	// Idle closes a connection that has carried no frame for this long.
	Idle time.Duration

	// Write bounds a single payload write. Applied by engine.Conn.
	Write time.Duration
}

// Config is everything a worker needs beyond its connection.
type Config struct {
	Registry *engine.Registry
	Handler  func(context.Context, *request.Request)
	Logger   logger.Logger
	Timeouts Timeouts

	// BaseContext is the parent of this connection's context. Nil means
	// context.Background(), so a zero-value Config behaves as it always has.
	BaseContext context.Context

	// Done is closed when the agent begins draining. Nil means this worker
	// never drains: a non-blocking receive on a nil channel always takes the
	// default branch, so a zero-value Config behaves as it always has.
	Done <-chan struct{}
}

// Handle listen connection and process frames
func Handle(conn *engine.Conn, cfg Config) {
	base := cfg.BaseContext
	if base == nil {
		base = context.Background()
	}

	ctx, cancel := context.WithCancel(base)

	w := &worker{
		conn:     conn,
		registry: cfg.Registry,
		handler:  cfg.Handler,
		logger:   cfg.Logger,
		timeouts: cfg.Timeouts,
		ctx:      ctx,
		cancel:   cancel,
		done:     cfg.Done,
	}

	if err := w.run(); err != nil {
		cfg.Logger.Errorf("handle worker: %v", err)
	}
}

type worker struct {
	conn     *engine.Conn
	registry *engine.Registry
	engine   *engine.Engine
	ready    bool
	engineID string
	handler  func(context.Context, *request.Request)

	// Negotiated during the HELLO handshake. maxFrameSize is the value the
	// AGENT-HELLO announced and is recorded on the connection, which is what
	// bounds writes to it. Written before the first NOTIFY goroutine starts.
	peerCapabilities []string
	maxFrameSize     uint32

	// async records whether this connection's ACKs may be rerouted to a
	// sibling. Section 3.2.1 makes async "a symmectical capability [ ... ] To
	// be used, it must be supported by HAproxy and agents" (the same rule it
	// states for pipelining). engine-id alone only says the peer's
	// connections CAN be grouped -- HAProxy sends it unconditionally, whether
	// or not async is configured -- so async additionally requires the peer's
	// capabilities list to name it.
	async bool

	// The peer is owed at most one AGENT-DISCONNECT however many goroutines
	// reach the failure. The connection's own close is idempotent.
	disconnectOnce sync.Once

	// inflight counts the NOTIFY handlers dispatched from this connection. Add
	// is only ever called from the read-loop goroutine and Wait runs in that
	// same goroutine once the loop has stopped, so the WaitGroup reuse race
	// cannot occur -- not by careful locking, but by construction.
	inflight sync.WaitGroup

	// deliverable records whether a sibling can still carry an in-flight ACK
	// once this connection has left its engine. Written by leaveEngine and read
	// by awaitDeliverable, both as run unwinds on the read-loop goroutine.
	deliverable bool

	logger   logger.Logger
	timeouts Timeouts

	// ctx is cancelled as run exits. A handler still working on a connection
	// that has gone is computing a result nobody can receive.
	ctx    context.Context
	cancel context.CancelFunc

	done <-chan struct{}
}

func (w *worker) close() {
	if err := w.conn.Close(); err != nil {
		w.logger.Errorf("close connection: %v", err)
	}
}

// leaveEngine takes this connection out of its engine so no further ACK is
// routed to it, and records whether anything is left that could carry one.
// Safe before the handshake, when there is no engine yet.
func (w *worker) leaveEngine() {
	if w.engine == nil {
		// No engine means no sibling: async was not negotiated, or the
		// handshake never completed. This connection was the only route.
		w.deliverable = false
		return
	}

	// Leave reports the departure that emptied the engine. Until that one, a
	// sibling remains for Engine.Write to route an ACK to.
	w.deliverable = !w.registry.Leave(w.engine, w.conn)
}

// awaitDeliverable waits for in-flight handlers when a sibling can still carry
// their ACKs -- section 3.2.1's async failover is exactly the case where a
// handler outliving its own connection is still useful. When nothing can carry
// them it returns at once, and the cancel that follows stops handlers whose
// results nobody can receive.
//
// During a shutdown this wait is bounded: Agent.Shutdown's grace-period
// expiry cancels the base context, which cancels the handlers and lets the
// WaitGroup reach zero. Outside a shutdown there is no bound -- a connection
// that dies while an async sibling remains, running a handler that never
// returns, blocks here indefinitely, because this worker's own cancel (run's
// deferred call) only happens after this wait returns. That is the deliberate
// cost of not discarding a deliverable ACK.
func (w *worker) awaitDeliverable() {
	if !w.deliverable {
		return
	}

	w.inflight.Wait()
}

// disconnect reports an agent-side error to the peer before the connection is
// closed, as section 3.2.9 requires. The connection is being torn down either
// way, so a failure to send is logged rather than returned: it cannot change
// what happens next, and the error that prompted the disconnect is the one
// worth reporting.
//
// Only the first caller sends a frame. A NOTIFY goroutine that cannot write its
// ACK tears the connection down, which makes the read loop fail in turn; the
// peer is owed one AGENT-DISCONNECT describing the first error, not a second
// describing the consequence.
func (w *worker) disconnect(statusCode uint32, message string) {
	w.disconnectOnce.Do(func() {
		if err := w.sendAgentDisconnect(statusCode, message); err != nil {
			w.logger.Errorf("send AgentDisconnect frame: %v", err)
		}
	})
}

// frameLimit is the ceiling for frames in either direction. Before the
// handshake settles one, the library's own maximum applies.
func (w *worker) frameLimit() uint32 {
	if w.maxFrameSize == 0 {
		return frame.MaxFrameSize
	}

	return w.maxFrameSize
}

// readTimeout is the handshake bound until the HELLO completes and the idle
// bound afterwards, which is how one deadline serves both of section 2.2's
// directives.
func (w *worker) readTimeout() time.Duration {
	if !w.ready {
		return w.timeouts.Handshake
	}

	return w.timeouts.Idle
}

// shuttingDown reports whether the agent has begun draining. A nil done channel
// blocks forever, so the default branch is taken and the answer is false.
func (w *worker) shuttingDown() bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

// drain finishes the work already dispatched, lets its ACKs out, and says
// goodbye. Section 3.2.9 requires the socket to close just after that frame,
// which run's deferred close does.
func (w *worker) drain() error {
	w.inflight.Wait()
	w.disconnect(statusCodeNormal, "agent is shutting down")

	return nil
}

// armReadDeadline bounds the next frame read. A zero timeout clears any
// deadline rather than setting one in the past.
func (w *worker) armReadDeadline() error {
	// A drain that began while this loop was between reads: catching it here is
	// a fast path that saves the syscall of arming a deadline this loop would
	// only have to expire again. It is not what closes the shutdown-wakeup
	// race -- every interleaving this check catches is also caught by the
	// post-arm check below, which is the load-bearing one.
	if w.shuttingDown() {
		return w.conn.SetReadDeadline(time.Now())
	}

	var err error
	if d := w.readTimeout(); d > 0 {
		err = w.conn.SetReadDeadline(time.Now().Add(d))
	} else {
		err = w.conn.SetReadDeadline(time.Time{})
	}
	if err != nil {
		return err
	}

	// Re-check after arming: Shutdown's poke can land between the check above
	// and the SetReadDeadline just done, and the arm above -- Idle == 0 arms
	// time.Time{}, an idle timeout arms a later deadline -- would silently
	// erase or outlast that poke, parking this read with no way to wake it.
	// Agent.Shutdown closes `done` strictly before it pokes any connection's
	// deadline, so that ordering guarantees a poke able to land here already
	// has `done` closed, which is exactly what this second check observes. Do
	// not delete this as redundant with the check above: it is what makes the
	// poke's wakeup reliable rather than racy.
	if w.shuttingDown() {
		return w.conn.SetReadDeadline(time.Now())
	}

	return nil
}

// advertisedCapabilities is what this connection's AGENT-HELLO claims.
func (w *worker) advertisedCapabilities() string {
	if !w.async {
		return capabilityPipelining
	}

	return capabilityPipelining + "," + capabilityAsync
}

func (w *worker) run() error {
	defer w.cancel()
	defer w.close()
	defer w.awaitDeliverable()
	defer w.leaveEngine()

	var f *frame.Frame

	buf := bufio.NewReader(w.conn.Reader())

	for {
		f = frame.AcquireFrame()

		if err := w.armReadDeadline(); err != nil {
			frame.ReleaseFrame(f)
			return fmt.Errorf("error set read deadline: %v", err)
		}

		if err := f.ReadLimit(buf, w.frameLimit()); err != nil {
			frame.ReleaseFrame(f)
			if err != io.EOF {
				switch {
				// A drain wakes the read loop the same way an idle timeout
				// does, so the flag is what tells them apart. A shutdown must
				// never report itself as section 3.5's code 2.
				case errors.Is(err, os.ErrDeadlineExceeded) && w.shuttingDown():
					return w.drain()

				// The peer went quiet: before the handshake that is section
				// 2.2's "timeout hello", after it "timeout idle". Either way
				// section 3.5's code 2 names it.
				case errors.Is(err, os.ErrDeadlineExceeded):
					w.disconnect(statusCodeTimeout, "timeout waiting for a frame")

				// A frame above the negotiated ceiling is code 3's own
				// condition.
				case errors.Is(err, frame.ErrFrameTooBig):
					w.disconnect(statusCodeFrameTooBig, err.Error())

				// Anything else Read rejects is a malformed frame.
				default:
					w.disconnect(statusCodeInvalidFrame, "invalid frame received")
				}

				return fmt.Errorf("error read frame: %v", err)
			}
			return nil
		}

		switch f.Type {
		case frame.TypeHAProxyHello:

			if w.ready {
				w.disconnect(statusCodeInvalidFrame, "unexpected HAProxyHello frame, handshake already completed")
				return fmt.Errorf("worker already ready, but got HAProxyHello frame")
			}

			agreed, disconnectErr := negotiate(f)
			if disconnectErr != nil {
				frame.ReleaseFrame(f)
				w.disconnect(disconnectErr.code, disconnectErr.message)
				return fmt.Errorf("handshake failed: %w", disconnectErr)
			}

			w.peerCapabilities = agreed.capabilities
			w.maxFrameSize = agreed.maxFrameSize
			w.conn.SetMaxFrameSize(agreed.maxFrameSize)
			w.engineID = f.EngineID

			// Section 3.2.1 defines async as symmetrical, like pipelining: "To
			// be used, it must be supported by HAproxy and agents." engine-id
			// says the peer's connections CAN be grouped; the capability list
			// says it will accept an ACK on a sibling. HAProxy sends engine-id
			// unconditionally, so it alone proves nothing.
			w.async = w.engineID != "" && slices.Contains(w.peerCapabilities, capabilityAsync)

			if err := w.sendAgentHello(f, agreed); err != nil {
				frame.ReleaseFrame(f)
				// The AGENT-HELLO could not be written, so an AGENT-DISCONNECT
				// cannot be either. Section 3.2.3's "error during the HELLO
				// handshake" sequence is unreachable once the socket is failing.
				return fmt.Errorf("error send AgentHello frame: %v", err)
			}

			if f.Healthcheck {
				frame.ReleaseFrame(f)
				return nil
			}

			w.ready = true
			if w.async {
				w.engine = w.registry.Join(w.engineID, w.conn)
			}
			continue

		case frame.TypeHAProxyDisconnect:
			if !w.ready {
				w.disconnect(statusCodeInvalidFrame, "unexpected HAProxyDisconnect frame before the handshake")
				return fmt.Errorf("worker not ready, but got HAProxyDisconnect frame")
			}

			if err := w.sendAgentDisconnect(statusCodeNormal, "connection closed by server"); err != nil {
				return fmt.Errorf("error send AgentDisconnect frame: %v", err)
			}
			frame.ReleaseFrame(f)
			return nil

		case frame.TypeNotify:
			if !w.ready {
				w.disconnect(statusCodeInvalidFrame, "unexpected Notify frame before the handshake")
				return fmt.Errorf("worker not ready, but got Notify frame")
			}

			w.inflight.Add(1)
			go w.processNotifyFrame(f)

		default:
			w.logger.Errorf("unexpected frame type: %v", f.Type)
		}
	}
}
