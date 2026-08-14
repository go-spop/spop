package transport

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

func TestConnWritePayloadSerialisesConcurrentWriters(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := NewConn(server)

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

func TestConnCloseIsIdempotent(t *testing.T) {
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

func TestConnWritePayloadFailsWhenClosed(t *testing.T) {
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

func TestConnWriteFrameWritesEveryByte(t *testing.T) {
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

func TestConnWriteFrameFailsWhenClosed(t *testing.T) {
	_, server := net.Pipe()

	c := NewConn(server)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := c.WriteFrame([]byte{0x01}); err == nil {
		t.Fatal("expected an error writing to a closed connection, got nil")
	}
}

func TestConnWritePayloadHonoursTheWriteTimeout(t *testing.T) {
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

func TestConnWritePayloadZeroTimeoutDoesNotExpire(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := NewConn(server)

	want := []byte{0x00, 0x00, 0x00, 0x01, 0x65}

	done := make(chan error, 1)
	go func() {
		done <- c.WriteFrame(want)
	}()

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

func TestConnWritePayloadClearsTheDeadlineAfterwards(t *testing.T) {
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

func TestConnWriteTimeoutRoundTrips(t *testing.T) {
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

func TestConnSetReadDeadline(t *testing.T) {
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
