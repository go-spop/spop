package engine

import "sync"

// Registry maps an engine-id to the engine grouping its connections.
//
// Section 3.2.4 makes engine-id optional, and section 3.2.1 names it as the
// only value an agent groups connections by. A connection that arrives without
// one therefore gets an engine of its own that nothing else can join: grouping
// unidentified connections together could route an ACK to a HAProxy that never
// sent the NOTIFY.
type Registry struct {
	mu      sync.Mutex
	engines map[string]*Engine

	// unidentified holds the singleton engines of connections with no
	// engine-id, so Len accounts for them.
	unidentified int
}

func NewRegistry() *Registry {
	return &Registry{engines: make(map[string]*Engine)}
}

// Join adds a connection to the engine for engineID, creating it if needed, and
// returns the engine the caller should write through.
func (r *Registry) Join(engineID string, c *Conn) *Engine {
	r.mu.Lock()
	defer r.mu.Unlock()

	if engineID == "" {
		r.unidentified++

		e := newEngine("")
		e.Add(c)

		return e
	}

	e, ok := r.engines[engineID]
	if !ok {
		e = newEngine(engineID)
		r.engines[engineID] = e
	}

	e.Add(c)

	return e
}

// Leave removes a connection and forgets the engine once its last connection
// has gone. It reports whether this connection was the last, which is how a
// worker learns that no sibling remains to carry an in-flight ACK.
func (r *Registry) Leave(e *Engine, c *Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	empty := e.Remove(c)
	if !empty {
		return false
	}

	if e.ID() == "" {
		r.unidentified--
		return true
	}

	// Only forget this engine if the map still holds it. This is defensive: as
	// long as an engine is dropped from the map exactly when it empties, and
	// Join only ever hands out the engine the map holds, a replaced engine can
	// never regain a member; so Remove cannot report "just emptied" for one.
	// The guard costs a map lookup and keeps that reasoning from becoming load
	// bearing.
	if current, ok := r.engines[e.ID()]; ok && current == e {
		delete(r.engines, e.ID())
	}

	return true
}

func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.engines) + r.unidentified
}
