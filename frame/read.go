package frame

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/go-spop/spop/varint"
)

// ErrFrameTooBig reports a frame whose declared length is above the maximum the
// reader was given. Section 3.5 reserves status code 3 for this condition.
var ErrFrameTooBig = errors.New("frame is too big")

// Read decodes a frame, bounded by MaxFrameSize. Once a maximum has been
// negotiated with the peer, use ReadLimit to apply it.
func (f *Frame) Read(src io.Reader) error {
	return f.ReadLimit(src, MaxFrameSize)
}

// ReadLimit decodes a frame, rejecting any whose declared length is above
// limit. Section 3.2: "Frames cannot exceed a maximum size negotiated between
// HAProxy and agents during the HELLO handshake." MaxFrameSize still applies as
// an absolute ceiling, so a limit above it is capped rather than trusted.
func (f *Frame) ReadLimit(src io.Reader, limit uint32) error {
	var n int
	var err error

	limit = min(limit, MaxFrameSize)

	n, err = io.ReadFull(src, f.tmp[:])
	if err != nil {
		if err == io.EOF {
			return err
		}
		return fmt.Errorf("error read frame size, %w", err)
	}

	f.Len = binary.BigEndian.Uint32(f.tmp[0:4])
	f.Type = Type(f.tmp[4])

	// Drop packet that doesn't have defined frame type early, before allocating any buffers
	// that way spurious connections (say someone calling curl on port) won't cause it to
	// allocate gigabytes of RAM
	switch f.Type {
	case TypeHAProxyHello, TypeHAProxyDisconnect, TypeNotify, TypeAgentHello, TypeAgentDisconnect, TypeAgentAck:
	default:
		return fmt.Errorf("unexpected frame type %d", f.Type)
	}

	if f.Len < minFrameLen {
		return fmt.Errorf("invalid frame length %d", f.Len)
	}

	if f.Len > limit {
		return fmt.Errorf("%w: %d exceeds the %d byte maximum", ErrFrameTooBig, f.Len, limit)
	}

	buf := make([]byte, f.Len-1)

	n, err = io.ReadFull(src, buf)
	if err != nil {
		return fmt.Errorf("error read frame, %w", err)
	}

	if uint32(n) != f.Len-1 {
		return fmt.Errorf("unexpected frame length %d, expect %d", n, f.Len)
	}

	f.Flags = binary.BigEndian.Uint32(buf[0:4])
	buf = buf[4:]

	f.StreamID, n = varint.Uvarint(buf)
	if n < 0 {
		return fmt.Errorf("truncated STREAM-ID varint")
	}
	buf = buf[n:]

	f.FrameID, n = varint.Uvarint(buf)
	if n < 0 {
		return fmt.Errorf("truncated FRAME-ID varint")
	}
	buf = buf[n:]

	switch f.Type {
	case TypeHAProxyHello, TypeHAProxyDisconnect:
		if err = f.KV.Unmarshal(buf); err != nil {
			return err
		}
		// Each KV-VALUE carries its own TYPED-DATA type nibble, so the type of
		// a received item is chosen by the peer. Assert rather than trust: a
		// mistyped item is malformed input, not a value to convert.
		if v, ok := f.KV.Get("healthcheck"); ok {
			healthcheck, ok := v.(bool)
			if !ok {
				return fmt.Errorf("expected BOOLEAN for healthcheck, got %T", v)
			}
			f.Healthcheck = healthcheck
		}
		if v, ok := f.KV.Get("max-frame-size"); ok {
			maxFrameSize, ok := v.(uint32)
			if !ok {
				return fmt.Errorf("expected UINT32 for max-frame-size, got %T", v)
			}
			f.MaxFrameSize = maxFrameSize
		}
		if v, ok := f.KV.Get("engine-id"); ok {
			engineID, ok := v.(string)
			if !ok {
				return fmt.Errorf("expected STRING for engine-id, got %T", v)
			}
			f.EngineID = engineID
		}

	case TypeNotify:
		err = f.Messages.Decode(buf)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("unexpected frame type %d", f.Type)
	}

	return nil
}
