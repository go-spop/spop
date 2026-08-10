package frame

import (
	"bytes"
	"testing"

	"github.com/go-spop/spop/payload/kv"
	"github.com/go-spop/spop/varint"
)

// Every KV-VALUE in a HAPROXY-HELLO carries its own TYPED-DATA type nibble, so
// the Go type Decode produces is chosen by the peer rather than by §3.2.4's
// schema. Frame.Read must treat a mistyped item as untrusted input and return
// an error; it must never assert the expected type directly on the decoded
// value.

// helloFrame assembles a HAPROXY-HELLO whose KV-LIST holds a single item with
// the given name and value, encoded through the library's own typed-data
// encoder so the wire bytes are well-formed in every respect but the type.
func helloFrame(t *testing.T, frameType Type, name string, value any) []byte {
	t.Helper()

	k := kv.NewKV()
	k.Add(name, value)

	payload, err := k.Bytes()
	if err != nil {
		t.Fatalf("encoding the KV-LIST failed: %v", err)
	}

	tmp := make([]byte, 10)

	body := []byte{byte(frameType)}
	body = append(body, 0x00, 0x00, 0x00, 0x01) // FLAGS: FIN
	body = append(body, tmp[:varint.PutUvarint(tmp, 0)]...)
	body = append(body, tmp[:varint.PutUvarint(tmp, 0)]...)
	body = append(body, payload...)

	out := []byte{
		byte(len(body) >> 24), byte(len(body) >> 16),
		byte(len(body) >> 8), byte(len(body)),
	}

	return append(out, body...)
}

func TestFrame_Read_helloMistypedKVValue(t *testing.T) {
	tests := []struct {
		name  string
		item  string
		value any
	}{
		{"healthcheck as string", "healthcheck", "true"},
		{"healthcheck as uint32", "healthcheck", uint32(1)},
		{"healthcheck as null", "healthcheck", nil},
		{"max-frame-size as string", "max-frame-size", "16384"},
		{"max-frame-size as int32", "max-frame-size", int32(16384)},
		{"max-frame-size as null", "max-frame-size", nil},
		{"engine-id as uint32", "engine-id", uint32(7)},
		{"engine-id as binary", "engine-id", []byte("abc")},
		{"engine-id as null", "engine-id", nil},
	}

	// The same three lookups run for HAPROXY-DISCONNECT, so a peer reaches them
	// with either frame type.
	for _, frameType := range []Type{TypeHAProxyHello, TypeHAProxyDisconnect} {
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				input := helloFrame(t, frameType, tc.item, tc.value)

				f := NewFrame()
				err := f.Read(bytes.NewReader(input))
				if err == nil {
					t.Fatalf("expected an error for %q carrying the wrong type, got nil", tc.item)
				}
			})
		}
	}
}

// The well-typed items must still decode, so the guard rejects only the
// mistyped case.
func TestFrame_Read_helloWellTypedKVValue(t *testing.T) {
	t.Run("healthcheck", func(t *testing.T) {
		f := NewFrame()
		if err := f.Read(bytes.NewReader(helloFrame(t, TypeHAProxyHello, "healthcheck", true))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !f.Healthcheck {
			t.Fatal("expected Healthcheck to be true")
		}
	})

	t.Run("max-frame-size", func(t *testing.T) {
		f := NewFrame()
		if err := f.Read(bytes.NewReader(helloFrame(t, TypeHAProxyHello, "max-frame-size", uint32(16384)))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.MaxFrameSize != 16384 {
			t.Fatalf("expected MaxFrameSize 16384, got %d", f.MaxFrameSize)
		}
	})

	t.Run("engine-id", func(t *testing.T) {
		f := NewFrame()
		if err := f.Read(bytes.NewReader(helloFrame(t, TypeHAProxyHello, "engine-id", "abc"))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.EngineID != "abc" {
			t.Fatalf("expected EngineID \"abc\", got %q", f.EngineID)
		}
	})

	// A healthcheck of false is well-typed and must not be rejected, nor set
	// the flag.
	t.Run("healthcheck false", func(t *testing.T) {
		f := NewFrame()
		if err := f.Read(bytes.NewReader(helloFrame(t, TypeHAProxyHello, "healthcheck", false))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.Healthcheck {
			t.Fatal("expected Healthcheck to be false")
		}
	})

	// An item outside the three the switch reads is ignored whatever its type:
	// the guard must reject only the items it actually converts.
	t.Run("unrelated item of an unexpected type", func(t *testing.T) {
		f := NewFrame()
		if err := f.Read(bytes.NewReader(helloFrame(t, TypeHAProxyHello, "capabilities", []byte("pipelining")))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
