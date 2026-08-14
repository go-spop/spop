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

		// An ACK the handler made too large cannot be fragmented, so it cannot
		// be sent at all. Section 3.2.9 has the agent report the error and
		// close rather than leave HAProxy waiting on an ACK that will never
		// arrive.
		if errors.Is(err, frame.ErrFrameTooBig) {
			w.disconnect(statusCodeFrameTooBig, err.Error())
			w.close()
		}
	}
}

func (w *worker) writeFrame(f *frame.Frame) error {
	buf := bytes.NewBuffer(make([]byte, 0))
	n, err := f.Encode(buf)
	if err != nil {
		return fmt.Errorf("cannot marshal frame: %w", err)
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
