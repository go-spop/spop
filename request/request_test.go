package request

import (
	"testing"

	"github.com/go-spop/spop/message"
)

// borrowedMessages stands in for the Messages a Frame owns and a Request only
// points at. Built from the pool, as a decoded frame's are, so releasing it is
// legal; Messages.Reset returns every entry to the message pool.
func borrowedMessages(t *testing.T) *message.Messages {
	t.Helper()

	m := message.AcquireMessage()
	m.Name = "check-client-ip"

	owned := message.NewMessages()
	*owned = append(*owned, m)

	// The owner releases them; nothing else may.
	t.Cleanup(owned.Reset)

	return owned
}

// A Request borrows the Messages of the frame it describes; it does not own
// them. Reset must drop the reference rather than reach into an object
// belonging to someone else; Messages.Reset returns every entry to the
// message pool, so a Request doing it is releasing the frame's messages out
// from under the frame.
func TestRequest_ResetDropsTheBorrowedMessages(t *testing.T) {
	borrowed := borrowedMessages(t)

	req := AcquireRequest()
	req.Messages = borrowed

	req.Reset()

	if req.Messages != nil {
		t.Fatal("expected Reset to drop the borrowed Messages reference")
	}

	if borrowed.Len() != 1 {
		t.Fatalf("expected the owner's Messages to be untouched, got %d entries", borrowed.Len())
	}
}

// The same through the pool's own entry point, which is how every caller
// reaches it.
func TestRequest_ReleaseDoesNotResetTheOwnersMessages(t *testing.T) {
	borrowed := borrowedMessages(t)

	req := AcquireRequest()
	req.Messages = borrowed

	ReleaseRequest(req)

	if borrowed.Len() != 1 {
		t.Fatalf("expected the owner's Messages to survive the release, got %d entries", borrowed.Len())
	}
}

// The rest of Reset must still clear, or a pooled Request leaks one request's
// identifiers into the next.
func TestRequest_ResetClearsEverythingElse(t *testing.T) {
	req := AcquireRequest()

	req.EngineID = "engine-1"
	req.StreamID = 7
	req.FrameID = 9

	req.Reset()

	if req.EngineID != "" || req.StreamID != 0 || req.FrameID != 0 {
		t.Fatalf("expected the identifiers cleared, got %q %d %d", req.EngineID, req.StreamID, req.FrameID)
	}
}
