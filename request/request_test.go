package request

import (
	"testing"

	"github.com/go-spop/spop/message"
)

func TestRequestResetDropsTheBorrowedMessages(t *testing.T) {
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

func TestRequestReleaseDoesNotResetTheOwnersMessages(t *testing.T) {
	borrowed := borrowedMessages(t)

	req := AcquireRequest()
	req.Messages = borrowed

	ReleaseRequest(req)

	if borrowed.Len() != 1 {
		t.Fatalf("expected the owner's Messages to survive the release, got %d entries", borrowed.Len())
	}
}

func TestRequestResetClearsEverythingElse(t *testing.T) {
	req := AcquireRequest()

	req.EngineID = "engine-1"
	req.StreamID = 7
	req.FrameID = 9

	req.Reset()

	if req.EngineID != "" || req.StreamID != 0 || req.FrameID != 0 {
		t.Fatalf("expected the identifiers cleared, got %q %d %d", req.EngineID, req.StreamID, req.FrameID)
	}
}

func borrowedMessages(t *testing.T) *message.Messages {
	t.Helper()

	m := message.AcquireMessage()
	m.Name = "check-client-ip"

	owned := message.NewMessages()
	*owned = append(*owned, m)

	t.Cleanup(owned.Reset)

	return owned
}
