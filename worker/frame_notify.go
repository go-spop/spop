package worker

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/go-spop/spop/frame"
	"github.com/go-spop/spop/request"
)

func (w *worker) processNotifyFrame(f *frame.Frame) {
	defer w.inflight.Done()

	// Released once the handler has returned AND its ACK has been written.
	// Releasing at handler return would admit a new NOTIFY while the previous
	// ACK was still going out, which is not what "in flight" means.
	defer w.releaseSlot()

	defer frame.ReleaseFrame(f)

	req := request.AcquireRequest()
	defer request.ReleaseRequest(req)

	req.StreamID = f.StreamID
	req.FrameID = f.FrameID
	req.EngineID = w.engineID
	req.Messages = f.Messages

	w.handler(w.ctx, req)

	ackFrame := frame.AcquireFrame()
	defer frame.ReleaseFrame(ackFrame)

	ackFrame.Type = frame.TypeAgentAck
	ackFrame.StreamID = f.StreamID
	ackFrame.FrameID = f.FrameID
	ackFrame.Actions = req.Actions

	err := w.writeFrame(ackFrame)
	if err != nil {
		w.logger.Errorf("ack frame write failed: %v", err)

		// However it failed, this ACK is not arriving, and section 3.2.9 has
		// the agent report an error rather than leave HAProxy waiting on a
		// frame that is not coming. Closing is not only about the one ACK: a
		// write that failed part way through has put a partial frame on the
		// wire, and every frame after it would be read against the wrong
		// offset. Reading on as if nothing had happened is the one thing this
		// connection cannot do.
		//
		// The disconnect goes first, while the socket may still carry it. When
		// the failure was the socket itself, that write fails in turn and is
		// logged, which costs nothing: disconnectOnce means the peer is owed no
		// second attempt.
		switch {
		// An ACK the handler made too large cannot be fragmented, so it cannot
		// be sent at all.
		case errors.Is(err, frame.ErrFrameTooBig):
			w.disconnect(statusCodeFrameTooBig, err.Error())

		// The frame never reached the wire: the handler produced actions this
		// agent cannot encode. Section 3.5 has no code for an agent-side bug,
		// so it is reported as the unknown error it is rather than as the I/O
		// error it is not.
		case errors.Is(err, errEncodeFrame):
			w.disconnect(statusCodeUnknown, err.Error())

		default:
			w.disconnect(statusCodeIOError, err.Error())
		}

		w.close()
	}
}

func (w *worker) writeFrame(f *frame.Frame) error {
	buf := bytes.NewBuffer(make([]byte, 0))
	n, err := f.Encode(buf)
	if err != nil {
		return fmt.Errorf("%w: %w", errEncodeFrame, err)
	}

	// FRAME-LENGTH excludes its own 4 prefix bytes, so the negotiated ceiling
	// is compared against the same quantity Read checks on the way in. This is
	// the ceiling of the connection the NOTIFY arrived on: an ACK too big for
	// that session is a handler error, not something to reroute past.
	if length := uint32(n - frameLengthPrefix); length > w.frameLimit() {
		return fmt.Errorf("%w: %d exceeds the %d byte maximum", frame.ErrFrameTooBig, length, w.frameLimit())
	}

	// Every frame goes out on the connection it belongs to. Section 3.2.1's
	// "async" capability would have allowed an ACK to leave on a sibling of the
	// same engine; this agent does not advertise it, so an ACK whose own
	// connection has gone is dropped rather than rerouted.
	return w.conn.WriteFrame(buf.Bytes())
}
