package message

import (
	"fmt"

	"github.com/go-spop/spop/varint"
)

func (m *Messages) Decode(buf []byte) error {
	for {
		if len(buf) == 0 {
			break
		}

		message := AcquireMessage()

		messageNameLen, n := varint.Uvarint(buf)
		if n < 0 {
			return fmt.Errorf("error read message name length: %w", varint.ErrTruncated)
		}
		buf = buf[n:]
		if len(buf) < int(messageNameLen) {
			return fmt.Errorf("error read message name: buffer too small")
		}
		message.Name = string(buf[:messageNameLen])
		buf = buf[messageNameLen:]

		if len(buf) == 0 {
			return fmt.Errorf("error read message arg count: buffer too small")
		}
		nbArgs := int(buf[0])
		buf = buf[1:]

		n, err := message.KV.UnmarshalNB(buf, nbArgs)

		if err != nil {
			return err
		}

		buf = buf[n:]

		*m = append(*m, message)
	}

	return nil
}
