package engine

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// WritePayload holds the connection for the whole callback. Fragmentation will
// later emit several frames inside one call, and SPOE 2.0 section 3.2 forbids
// interleaving frames of different payloads on one connection; so bytes from
// two concurrent callbacks must never overlap.
func TestConn_WritePayload_serialisesConcurrentWriters(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := NewConn(server)

	// Each writer emits its own byte 64 times in two separate Write calls. If
	// the lock does not span the callback, the two runs interleave.
	const writers = 8
	const runLength = 64

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = c.WritePayload(func(w io.Writer) error {
				half := bytes.Repeat([]byte{byte('a' + id)}, runLength/2)
				if _, err := w.Write(half); err != nil {
					return err
				}

				// Without the lock this pause hands the connection to every
				// other writer, so the two halves are guaranteed to be split
				// apart. Holding the lock, it only makes the test take
				// writers*pause to run. Relying on the scheduler instead would
				// make this a coin flip; it caught a missing lock about half
				// the time.
				time.Sleep(2 * time.Millisecond)

				_, err := w.Write(half)
				return err
			})
		}(i)
	}

	got := make([]byte, writers*runLength)
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(client, got)
		done <- err
	}()

	wg.Wait()
	if err := <-done; err != nil {
		t.Fatalf("reading the written bytes: %v", err)
	}

	for i := 0; i < len(got); i += runLength {
		run := got[i : i+runLength]
		if bytes.Count(run, run[:1]) != runLength {
			t.Fatalf("payload at offset %d is interleaved: %q", i, run)
		}
	}
}

func TestConn_Close_isIdempotent(t *testing.T) {
	_, server := net.Pipe()

	c := NewConn(server)

	if c.IsClosed() {
		t.Fatal("a new connection reports itself closed")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	if !c.IsClosed() {
		t.Fatal("a closed connection does not report itself closed")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("second close returned an error: %v", err)
	}
}

// A write to a closed connection must fail rather than block or panic.
func TestConn_WritePayload_failsWhenClosed(t *testing.T) {
	_, server := net.Pipe()

	c := NewConn(server)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	err := c.WritePayload(func(w io.Writer) error {
		_, err := w.Write([]byte{0x01})
		return err
	})
	if err == nil {
		t.Fatal("expected an error writing to a closed connection, got nil")
	}
}

func TestConn_WriteFrame_writesEveryByte(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := NewConn(server)

	want := []byte{0x00, 0x00, 0x00, 0x01, 0x65}

	got := make([]byte, len(want))
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(client, got)
		done <- err
	}()

	if err := c.WriteFrame(want); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("reading the frame: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestConn_WriteFrame_failsWhenClosed(t *testing.T) {
	_, server := net.Pipe()

	c := NewConn(server)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := c.WriteFrame([]byte{0x01}); err == nil {
		t.Fatal("expected an error writing to a closed connection, got nil")
	}
}

func TestConn_MaxFrameSize_roundTrips(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()

	c := NewConn(server)
	if got := c.MaxFrameSize(); got != 0 {
		t.Fatalf("expected 0 before negotiation, got %d", got)
	}

	c.SetMaxFrameSize(16384)

	if got := c.MaxFrameSize(); got != 16384 {
		t.Fatalf("expected 16384, got %d", got)
	}
}

// A peer that never reads must not pin a writer forever. net.Pipe is
// unbuffered, so a Write with no reader blocks until the deadline fires; an
// exact model of a wedged peer.
func TestConn_WritePayload_honoursTheWriteTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := NewConn(server)
	c.SetWriteTimeout(50 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		done <- c.WriteFrame([]byte{0x00, 0x00, 0x00, 0x01, 0x65})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("expected a deadline error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the write never returned; the timeout was not applied")
	}
}

// Zero means no deadline, so a write waits for its reader however long that
// takes.
func TestConn_WritePayload_zeroTimeoutDoesNotExpire(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := NewConn(server)

	want := []byte{0x00, 0x00, 0x00, 0x01, 0x65}

	done := make(chan error, 1)
	go func() {
		done <- c.WriteFrame(want)
	}()

	// Read only after a pause the write would not survive if a deadline of the
	// previous test's length were somehow in force.
	time.Sleep(100 * time.Millisecond)

	got := make([]byte, len(want))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("reading the frame: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the write never returned")
	}
}

// A deadline must not leak into the next write on the same connection.
func TestConn_WritePayload_clearsTheDeadlineAfterwards(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := NewConn(server)
	c.SetWriteTimeout(2 * time.Second)

	want := []byte{0x00, 0x00, 0x00, 0x01, 0x65}

	for i := 0; i < 2; i++ {
		done := make(chan error, 1)
		go func() {
			done <- c.WriteFrame(want)
		}()

		got := make([]byte, len(want))
		if _, err := io.ReadFull(client, got); err != nil {
			t.Fatalf("write %d, reading the frame: %v", i, err)
		}

		if err := <-done; err != nil {
			t.Fatalf("write %d returned %v", i, err)
		}
	}
}

func TestConn_WriteTimeout_roundTrips(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()

	c := NewConn(server)
	if got := c.WriteTimeout(); got != 0 {
		t.Fatalf("expected 0 before configuration, got %v", got)
	}

	c.SetWriteTimeout(3 * time.Second)

	if got := c.WriteTimeout(); got != 3*time.Second {
		t.Fatalf("expected 3s, got %v", got)
	}
}

// The read loop drives its own deadline through the connection.
func TestConn_SetReadDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := NewConn(server)

	if err := c.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("setting the deadline: %v", err)
	}

	buf := make([]byte, 1)
	if _, err := c.Reader().Read(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected a deadline error, got %v", err)
	}
}
