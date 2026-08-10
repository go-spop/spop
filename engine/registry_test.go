package engine

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
)

func newTestConn(t *testing.T) *Conn {
	t.Helper()

	client, server := net.Pipe()
	t.Cleanup(func() { client.Close() })

	c := NewConn(server)
	c.SetMaxFrameSize(16384)

	return c
}

func TestRegistry_Join_groupsBySharedEngineID(t *testing.T) {
	r := NewRegistry()

	first := newTestConn(t)
	second := newTestConn(t)

	a := r.Join("engine-1", first)
	b := r.Join("engine-1", second)

	if a != b {
		t.Fatal("connections sharing an engine-id joined different engines")
	}

	if r.Len() != 1 {
		t.Fatalf("expected 1 engine, got %d", r.Len())
	}
}

func TestRegistry_Join_separatesDifferentEngineIDs(t *testing.T) {
	r := NewRegistry()

	a := r.Join("engine-1", newTestConn(t))
	b := r.Join("engine-2", newTestConn(t))

	if a == b {
		t.Fatal("connections with different engine-ids joined the same engine")
	}

	if r.Len() != 2 {
		t.Fatalf("expected 2 engines, got %d", r.Len())
	}
}

// engine-id is optional. Without one there is nothing safe to group by: two
// such connections may be different HAProxy instances entirely.
func TestRegistry_Join_givesEachUnidentifiedConnectionItsOwnEngine(t *testing.T) {
	r := NewRegistry()

	a := r.Join("", newTestConn(t))
	b := r.Join("", newTestConn(t))

	if a == b {
		t.Fatal("two connections with no engine-id were grouped together")
	}

	if r.Len() != 2 {
		t.Fatalf("expected 2 singleton engines, got %d", r.Len())
	}
}

func TestRegistry_Leave_forgetsTheEngineWhenItsLastConnectionGoes(t *testing.T) {
	r := NewRegistry()

	first := newTestConn(t)
	second := newTestConn(t)

	e := r.Join("engine-1", first)
	r.Join("engine-1", second)

	r.Leave(e, first)
	if r.Len() != 1 {
		t.Fatalf("the engine was forgotten while a connection remained: %d engines", r.Len())
	}

	r.Leave(e, second)
	if r.Len() != 0 {
		t.Fatalf("expected the engine to be forgotten, got %d engines", r.Len())
	}
}

// A connection joining the same engine-id after the engine was forgotten must
// get a working engine rather than a stale one.
func TestRegistry_Join_afterTheEngineWasForgotten(t *testing.T) {
	r := NewRegistry()

	first := newTestConn(t)
	e := r.Join("engine-1", first)
	r.Leave(e, first)

	second := newTestConn(t)
	rejoined := r.Join("engine-1", second)

	if rejoined == e {
		t.Fatal("a forgotten engine was handed out again")
	}

	if r.Len() != 1 {
		t.Fatalf("expected 1 engine, got %d", r.Len())
	}
}

// Leaving twice must not be counted twice. Remove reports "this call emptied
// the engine", so a repeated Leave -- from a duplicate teardown path, say --
// has to be a no-op rather than a second decrement.
func TestRegistry_Leave_isNotCountedTwice(t *testing.T) {
	r := NewRegistry()

	unidentified := newTestConn(t)
	singleton := r.Join("", unidentified)

	r.Leave(singleton, unidentified)
	r.Leave(singleton, unidentified)

	if got := r.Len(); got != 0 {
		t.Fatalf("expected 0 engines after leaving twice, got %d", got)
	}

	identified := newTestConn(t)
	e := r.Join("engine-1", identified)

	r.Leave(e, identified)
	r.Leave(e, identified)

	if got := r.Len(); got != 0 {
		t.Fatalf("expected 0 engines after leaving twice, got %d", got)
	}
}

// A stale Leave for an engine that has already been replaced must not evict its
// successor. Remove is what enforces this today -- it reports false for a
// connection it did not remove, so the repeated Leave returns early. The test
// pins the behaviour rather than the mechanism, so it still fails if either
// Remove's contract or Leave's guard regresses.
func TestRegistry_Leave_doesNotEvictAReplacementEngine(t *testing.T) {
	r := NewRegistry()

	first := newTestConn(t)
	stale := r.Join("engine-1", first)
	r.Leave(stale, first)

	second := newTestConn(t)
	live := r.Join("engine-1", second)

	// The stale engine is empty and no longer in the map; leaving it again must
	// not touch the entry the replacement now owns.
	r.Leave(stale, first)

	third := newTestConn(t)
	if rejoined := r.Join("engine-1", third); rejoined != live {
		t.Fatal("a stale Leave evicted the engine that replaced it")
	}
}

// TestRegistry_ConcurrentJoinWriteLeave exercises one shared Registry from
// many goroutines at once, each doing its own Join, Write and Leave against a
// handful of shared engine-ids -- the pattern many workers produce in
// practice. Correctness here does not depend on ordering or timing: every
// goroutine's Join is matched by its own Leave, so however the operations
// interleave, the registry must be empty once every goroutine has finished.
// Run with -race, this is what would catch an unsynchronised access to
// Registry.engines or Engine.conns.
func TestRegistry_ConcurrentJoinWriteLeave(t *testing.T) {
	r := NewRegistry()

	const engineIDs = 5
	const perEngineID = 20

	var wg sync.WaitGroup

	for i := 0; i < engineIDs*perEngineID; i++ {
		engineID := fmt.Sprintf("engine-%d", i%engineIDs)

		wg.Add(1)
		go func(engineID string) {
			defer wg.Done()

			client, server := net.Pipe()
			defer client.Close()

			// Drain whatever the Write below sends, so it does not block on
			// the pipe waiting for a reader that will never come.
			go io.Copy(io.Discard, client)

			c := NewConn(server)
			c.SetMaxFrameSize(16384)
			defer c.Close()

			e := r.Join(engineID, c)

			// A minimal well-formed frame: 4 bytes of FRAME-LENGTH, itself
			// declaring a zero-length body.
			if err := e.Write(c, []byte{0x00, 0x00, 0x00, 0x00}); err != nil {
				t.Errorf("writing through the engine: %v", err)
			}

			r.Leave(e, c)
		}(engineID)
	}

	wg.Wait()

	if got := r.Len(); got != 0 {
		t.Fatalf("expected the registry to be empty once every goroutine left, got %d engines", got)
	}
}
