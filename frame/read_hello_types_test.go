package frame

import (
	"bytes"
	"testing"

	"github.com/go-spop/spop/payload/kv"
	"github.com/go-spop/spop/varint"
)

func TestFrameReadHelloMistypedKVValue(t *testing.T) {
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

func TestFrameReadHelloWellTypedKVValue(t *testing.T) {
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

	t.Run("healthcheck false", func(t *testing.T) {
		f := NewFrame()
		if err := f.Read(bytes.NewReader(helloFrame(t, TypeHAProxyHello, "healthcheck", false))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.Healthcheck {
			t.Fatal("expected Healthcheck to be false")
		}
	})

	t.Run("unrelated item of an unexpected type", func(t *testing.T) {
		f := NewFrame()
		if err := f.Read(bytes.NewReader(helloFrame(t, TypeHAProxyHello, "capabilities", []byte("pipelining")))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

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
	body = append(body, 0x00, 0x00, 0x00, 0x01)
	body = append(body, tmp[:varint.PutUvarint(tmp, 0)]...)
	body = append(body, tmp[:varint.PutUvarint(tmp, 0)]...)
	body = append(body, payload...)

	out := []byte{
		byte(len(body) >> 24), byte(len(body) >> 16),
		byte(len(body) >> 8), byte(len(body)),
	}

	return append(out, body...)
}
