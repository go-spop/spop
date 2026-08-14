package varint

/*

From SPOE spec:

Variable-length integer (varint) are encoded using Peers encoding:


       0  <= X < 240        : 1 byte  (7.875 bits)  [ XXXX XXXX ]
      240 <= X < 2288       : 2 bytes (11 bits)     [ 1111 XXXX ] [ 0XXX XXXX ]
     2288 <= X < 264432     : 3 bytes (18 bits)     [ 1111 XXXX ] [ 1XXX XXXX ]   [ 0XXX XXXX ]
   264432 <= X < 33818864   : 4 bytes (25 bits)     [ 1111 XXXX ] [ 1XXX XXXX ]*2 [ 0XXX XXXX ]
 33818864 <= X < 4328786160 : 5 bytes (32 bits)     [ 1111 XXXX ] [ 1XXX XXXX ]*3 [ 0XXX XXXX ]
 ...

*/

// MaxLen is the largest number of bytes PutUvarint can need. The first byte
// carries 4 bits of the value and every byte after it carries 7, so a full
// 64-bit value takes ceil((64-4)/7)+1 bytes. A destination buffer of this size
// is never too small.
const MaxLen = 10

// PutUvarint encodes n into buf and returns the number of bytes written, or -1
// if buf is too small to hold the whole encoding.
//
// The value is built in a scratch array and copied over only once its full
// width is known, so a buffer that turns out to be too small is left exactly as
// it was. Writing as it went would leave the destination holding a prefix of an
// encoding that was never completed, indistinguishable from one that was.
//
// The scratch array cannot overflow: MaxLen is the widest encoding a uint64 has,
// which varint's own tests pin from both sides.
func PutUvarint(buf []byte, n uint64) int {
	var scratch [MaxLen]byte

	var p int

	if n < 240 {
		scratch[p] = byte(n)
		p++
	} else {
		scratch[p] = byte(n) | 0xF0
		p++

		n = (n - 240) >> 4

		for n >= 128 {
			scratch[p] = byte(n) | 128
			n = (n - 128) >> 7

			p++
		}

		scratch[p] = byte(n)
		p++
	}

	if len(buf) < p {
		return -1
	}

	copy(buf, scratch[:p])

	return p
}

func Uvarint(buf []byte) (uint64, int) {
	var p int

	if len(buf) == 0 {
		return 0, -1
	}

	n := uint64(buf[p])

	if n < 240 {
		return n, 1
	}

	r := uint(4)

	for {
		p++
		if p >= len(buf) {
			return 0, -1
		}
		n += uint64(buf[p]) << r
		r += 7
		if int64(buf[p]) < 128 {
			break
		}
	}

	return n, p + 1
}
