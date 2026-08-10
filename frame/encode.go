package frame

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/go-spop/spop/varint"
)

func (f *Frame) Encode(dest io.Writer) (n int, err error) {
	buf := bytes.Buffer{}

	// Reserve the FRAME-LENGTH prefix. Its value is only known once the payload
	// is encoded, so it is patched in below; building it into the same buffer
	// is what lets the whole frame reach dest in a single Write, rather than a
	// prefix that can land without the body behind it.
	var prefix [frameLengthPrefix]byte
	buf.Write(prefix[:])

	buf.WriteByte(byte(f.Type))

	binary.BigEndian.PutUint32(f.tmp[:], f.Flags)

	buf.Write(f.tmp[0:4])

	// Kept out of n, which reports bytes written to dest and nothing else, so
	// an early return below cannot report a varint's length as a write count.
	var varintLen int

	varintLen = varint.PutUvarint(f.varintBuf[:], f.StreamID)
	buf.Write(f.varintBuf[:varintLen])

	varintLen = varint.PutUvarint(f.varintBuf[:], f.FrameID)
	buf.Write(f.varintBuf[:varintLen])

	var payload []byte

	switch f.Type {
	case TypeAgentHello, TypeAgentDisconnect, TypeHAProxyHello, TypeHAProxyDisconnect:
		payload, err = f.KV.Bytes()
		if err != nil {
			return
		}

	case TypeAgentAck:
		if f.Actions != nil {
			for _, act := range f.Actions {
				payload, err = act.Marshal(payload)
				if err != nil {
					return
				}
			}
		}
	case TypeNotify:
		if len(*f.Messages) > 0 {
			err = fmt.Errorf("encoding Notify frame with Message isn't handled yet")
			return

		}
	default:
		err = fmt.Errorf("unexpected frame type %d", f.Type)
		return
	}

	buf.Write(payload)

	// FRAME-LENGTH counts everything after itself, which is the quantity Read
	// stores too, so both operations agree on what Len means.
	f.Len = uint32(buf.Len() - frameLengthPrefix)
	binary.BigEndian.PutUint32(buf.Bytes()[:frameLengthPrefix], f.Len)

	n, err = dest.Write(buf.Bytes())
	if err != nil || n != buf.Len() {
		// n is what dest actually took, so a caller can tell a frame that never
		// left from one cut short. %w keeps the writer's own error reachable by
		// errors.Is.
		return n, fmt.Errorf("error write frame. writes %d, expect %d, err: %w", n, buf.Len(), err)
	}

	return n, nil
}
