package typeddata

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"reflect"

	"github.com/go-spop/spop/varint"
)

const (
	// TypeNull const for TypedData type
	TypeNull byte = 0
	// TypeBoolean const for TypedData type
	TypeBoolean byte = 1
	// TypeInt32 const for TypedData type
	TypeInt32 byte = 2
	// TypeUInt32 const for TypedData type
	TypeUInt32 byte = 3
	// TypeInt64 const for TypedData type
	TypeInt64 byte = 4
	// TypeUInt64 const for TypedData type
	TypeUInt64 byte = 5
	// TypeIPv4 const for TypedData type
	TypeIPv4 byte = 6
	// TypeIPv6 const for TypedData type
	TypeIPv6 byte = 7
	// TypeString const for TypedData type
	TypeString byte = 8
	// TypeBinary const for TypedData type
	TypeBinary byte = 9
)

// ErrEmptyBuffer describe error, if passed empty buffer for decoding
var ErrEmptyBuffer = errors.New("empty buffer for decode")

// ErrDecodingBufferTooSmall describe error for too small decoding buffer
var ErrDecodingBufferTooSmall = errors.New("decoding buffer too small")

// ErrTruncatedVarint describes a varint whose continuation bytes are missing
var ErrTruncatedVarint = errors.New("truncated varint")

// ErrInvalidIP describes a net.IP that is neither of the two widths section 3.1
// gives a type to: IN_ADDR's 4 bytes and IN_ADDR6's 16.
var ErrInvalidIP = errors.New("net.IP is neither a 4-byte nor a 16-byte address")

// Encode variable to TypedData value
// returns filled buffer, count of bytes and error
func Encode(data any, buf []byte) ([]byte, int, error) {
	var n int

	switch v := data.(type) {
	case nil:
		buf = append(buf, TypeNull)
		return buf, 1, nil

	case bool:
		var b byte = 0x11
		if !v {
			b = 0x01
		}
		buf = append(buf, b)
		return buf, 1, nil

	case int32:
		buf = append(buf, TypeInt32)
		b := make([]byte, varint.MaxLen)
		i := varint.PutUvarint(b, uint64(v))
		buf = append(buf, b[:i]...)
		return buf, i + 1, nil

	case uint32:
		buf = append(buf, TypeUInt32)
		b := make([]byte, varint.MaxLen)
		i := varint.PutUvarint(b, uint64(v))
		buf = append(buf, b[:i]...)
		return buf, i + 1, nil

	case int:
		buf = append(buf, TypeInt64)
		b := make([]byte, varint.MaxLen)
		i := varint.PutUvarint(b, uint64(v))
		buf = append(buf, b[:i]...)
		return buf, i + 1, nil

	case int64:
		buf = append(buf, TypeInt64)
		b := make([]byte, varint.MaxLen)
		i := varint.PutUvarint(b, uint64(v))
		buf = append(buf, b[:i]...)
		return buf, i + 1, nil

	case uint:
		buf = append(buf, TypeUInt64)
		b := make([]byte, varint.MaxLen)
		i := varint.PutUvarint(b, uint64(v))
		buf = append(buf, b[:i]...)
		return buf, i + 1, nil

	case uint64:
		buf = append(buf, TypeUInt64)
		b := make([]byte, varint.MaxLen)
		i := varint.PutUvarint(b, uint64(v))
		buf = append(buf, b[:i]...)
		return buf, i + 1, nil

	case string:
		n = 1
		buf = append(buf, TypeString)
		b := make([]byte, varint.MaxLen)
		i := varint.PutUvarint(b, uint64(len(v)))
		n += i
		n += len(v)
		buf = append(buf, b[:i]...)
		buf = append(buf, v...)
		return buf, n, nil

	case net.IP:
		// Which type an address takes is decided by the address, not by the
		// width of the net.IP holding it: net.ParseIP keeps an IPv4 address in
		// the 16-byte v4-in-v6 form, and section 3.1's IN_ADDR is 4 bytes. To4
		// returns nil for anything that is not an IPv4 address, which is what
		// separates the two cases.
		if v4 := v.To4(); v4 != nil {
			buf = append(buf, TypeIPv4)
			buf = append(buf, v4...)

			return buf, 1 + net.IPv4len, nil
		}

		// To16 accepts the 4-byte form too, so the IPv4 case above has to come
		// first. What reaches here is a genuine IPv6 address or a net.IP of a
		// length section 3.1 has no type for, including the nil one.
		v6 := v.To16()
		if v6 == nil {
			return nil, 0, fmt.Errorf("%w: %d bytes", ErrInvalidIP, len(v))
		}

		buf = append(buf, TypeIPv6)
		buf = append(buf, v6...)

		return buf, 1 + net.IPv6len, nil

	case []byte:
		n = 1
		buf = append(buf, TypeBinary)
		b := make([]byte, varint.MaxLen)
		i := varint.PutUvarint(b, uint64(len(v)))
		n += i
		n += len(v)
		buf = append(buf, b[:i]...)
		buf = append(buf, v...)
		return buf, n, nil
	}

	return nil, 0, fmt.Errorf("type not supported for encode to TypedData: %s", reflect.TypeOf(data).String())
}

// Decode TypedData value
// Returns decoded variable, bytes count and error
func Decode(buf []byte) (data any, n int, err error) {
	if len(buf) == 0 {
		err = ErrEmptyBuffer
		return
	}

	f := buf[0] >> 4
	t := buf[0] & 0x0F
	buf = buf[1:]
	n = 1

	switch t {
	case TypeNull:
		return

	case TypeBoolean:
		data = f&0x01 > 0
		return

	case TypeInt32:
		i, l := varint.Uvarint(buf)
		if l < 0 {
			err = ErrTruncatedVarint
			return
		}
		n += l
		data = int32(i)
		return

	case TypeUInt32:
		i, l := varint.Uvarint(buf)
		if l < 0 {
			err = ErrTruncatedVarint
			return
		}
		n += l
		data = uint32(i)
		return

	case TypeInt64:
		i, l := varint.Uvarint(buf)
		if l < 0 {
			err = ErrTruncatedVarint
			return
		}
		n += l
		data = int64(i)
		return

	case TypeUInt64:
		i, l := varint.Uvarint(buf)
		if l < 0 {
			err = ErrTruncatedVarint
			return
		}
		n += l
		data = uint64(i)
		return

	case TypeIPv4:
		if len(buf) < net.IPv4len {
			err = ErrDecodingBufferTooSmall
			return
		}
		data = net.IP(bytes.Clone(buf[:net.IPv4len]))
		n += net.IPv4len
		return

	case TypeIPv6:
		if len(buf) < net.IPv6len {
			err = ErrDecodingBufferTooSmall
			return
		}
		data = net.IP(bytes.Clone(buf[:net.IPv6len]))
		n += net.IPv6len
		return

	case TypeString:
		sLen, i := varint.Uvarint(buf)
		if i < 0 {
			err = ErrTruncatedVarint
			return
		}
		n += i
		buf = buf[i:]
		if uint64(len(buf)) < sLen {
			err = ErrDecodingBufferTooSmall
			return
		}
		data = string(buf[:sLen])
		n += int(sLen)
		return

	case TypeBinary:
		dataLen, i := varint.Uvarint(buf)
		if i < 0 {
			err = ErrTruncatedVarint
			return
		}
		n += i
		buf = buf[i:]
		if uint64(len(buf)) < dataLen {
			err = ErrDecodingBufferTooSmall
			return
		}
		data = bytes.Clone(buf[:dataLen])
		n += int(dataLen)
		return
	}

	return nil, n, fmt.Errorf("type %d not supported for decode from TypedData", t)
}
