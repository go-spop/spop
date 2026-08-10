package worker

import (
	"bufio"
	"fmt"
	"io"
	"net"

	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
)

const (
	capabilities = "pipelining,async"
)

// Status codes from SPOE 2.0 section 3.5.
const (
	statusCodeNormal       uint32 = 0
	statusCodeInvalidFrame uint32 = 4
)

// Handle listen connection and process frames
func Handle(conn net.Conn, handler func(*request.Request), logger logger.Logger) {
	w := &worker{
		conn:    conn,
		handler: handler,
		logger:  logger,
	}

	if err := w.run(); err != nil {
		logger.Errorf("handle worker: %v", err)
	}
}

type worker struct {
	conn     net.Conn
	ready    bool
	engineID string
	handler  func(*request.Request)

	logger logger.Logger
}

func (w *worker) close() {
	if err := w.conn.Close(); err != nil {
		w.logger.Errorf("close connection: %v", err)
	}
}

// disconnect reports an agent-side error to the peer before the connection is
// closed, as section 3.2.9 requires. The connection is being torn down either
// way, so a failure to send is logged rather than returned: it cannot change
// what happens next, and the error that prompted the disconnect is the one
// worth reporting.
func (w *worker) disconnect(statusCode uint32, message string) {
	if err := w.sendAgentDisconnect(statusCode, message); err != nil {
		w.logger.Errorf("send AgentDisconnect frame: %v", err)
	}
}

func (w *worker) run() error {

	defer w.close()

	var f *frame.Frame

	buf := bufio.NewReader(w.conn)

	for {
		f = frame.AcquireFrame()

		if err := f.Read(buf); err != nil {
			frame.ReleaseFrame(f)
			if err != io.EOF {
				w.disconnect(statusCodeInvalidFrame, "invalid frame received")
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

			if err := w.sendAgentHello(f); err != nil {
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

			w.engineID = f.EngineID

			w.ready = true
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

			go w.processNotifyFrame(f)

		default:
			w.logger.Errorf("unexpected frame type: %v", f.Type)
		}
	}
}
