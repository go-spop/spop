package request

import (
	"sync"

	"github.com/go-spop/spop/action"
	"github.com/go-spop/spop/message"
)

var requestPool = sync.Pool{
	New: func() any {
		return newRequest()
	},
}

type Request struct {
	EngineID string
	StreamID uint64
	FrameID  uint64

	// Messages is BORROWED from the frame this request describes. The frame
	// owns them and releases them; a Request must never reset them, because
	// Messages.Reset returns every entry to the message pool; doing it here
	// would release the frame's messages out from under the frame. Valid only
	// for the duration of the handler call.
	Messages *message.Messages

	Actions action.Actions
}

func newRequest() *Request {
	// Messages is deliberately not allocated: it is always assigned from the
	// frame being handled, so anything allocated here would be discarded.
	m := &Request{
		Actions: make(action.Actions, 0, 1),
	}

	return m
}

func AcquireRequest() *Request {
	m := requestPool.Get()
	if m == nil {
		return newRequest()
	}

	return m.(*Request)
}

func ReleaseRequest(m *Request) {
	m.Reset()
	requestPool.Put(m)
}

func (req *Request) Reset() {
	// Dropped, not reset: see the field's comment. Dropping also stops a
	// pooled Request from retaining a pointer into a frame that has since gone
	// back to the frame pool.
	req.Messages = nil

	req.Actions.Reset()

	req.EngineID = ""
	req.StreamID = 0
	req.FrameID = 0
}
