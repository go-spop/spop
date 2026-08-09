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
			ReleaseMessage(message)
			return fmt.Errorf("error decode message, truncated name length varint")
		}
		buf = buf[n:]
		if uint64(len(buf)) < messageNameLen {
			ReleaseMessage(message)
			return fmt.Errorf("error decode message, name length %d exceeds %d remaining bytes", messageNameLen, len(buf))
		}
		message.Name = string(buf[:messageNameLen])
		buf = buf[messageNameLen:]

		if len(buf) == 0 {
			ReleaseMessage(message)
			return fmt.Errorf("error decode message %q, missing NB-ARGS byte", message.Name)
		}
		nbArgs := int(buf[0])
		buf = buf[1:]

		n, err := message.KV.UnmarshalNB(buf, nbArgs)

		if err != nil {
			ReleaseMessage(message)
			return err
		}

		buf = buf[n:]

		*m = append(*m, message)
	}

	return nil
}
