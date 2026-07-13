package sessions

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func NewID() (string, error) {
	ulid, err := newULID(time.Now().UTC())
	if err != nil {
		return "", err
	}

	return "session_" + ulid, nil
}

func newULID(t time.Time) (string, error) {
	var entropy [10]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}

	var value [16]byte
	millis := uint64(t.UnixMilli())
	value[0] = byte(millis >> 40)
	value[1] = byte(millis >> 32)
	value[2] = byte(millis >> 24)
	value[3] = byte(millis >> 16)
	value[4] = byte(millis >> 8)
	value[5] = byte(millis)
	copy(value[6:], entropy[:])

	return encodeULID(value), nil
}

func encodeULID(value [16]byte) string {
	var out [26]byte
	hi := binary.BigEndian.Uint64(value[0:8])
	lo := binary.BigEndian.Uint64(value[8:16])

	out[0] = crockfordBase32[(hi>>61)&0x1f]
	out[1] = crockfordBase32[(hi>>56)&0x1f]
	out[2] = crockfordBase32[(hi>>51)&0x1f]
	out[3] = crockfordBase32[(hi>>46)&0x1f]
	out[4] = crockfordBase32[(hi>>41)&0x1f]
	out[5] = crockfordBase32[(hi>>36)&0x1f]
	out[6] = crockfordBase32[(hi>>31)&0x1f]
	out[7] = crockfordBase32[(hi>>26)&0x1f]
	out[8] = crockfordBase32[(hi>>21)&0x1f]
	out[9] = crockfordBase32[(hi>>16)&0x1f]
	out[10] = crockfordBase32[(hi>>11)&0x1f]
	out[11] = crockfordBase32[(hi>>6)&0x1f]
	out[12] = crockfordBase32[(hi>>1)&0x1f]
	out[13] = crockfordBase32[((hi&0x1)<<4)|((lo>>60)&0x0f)]
	out[14] = crockfordBase32[(lo>>55)&0x1f]
	out[15] = crockfordBase32[(lo>>50)&0x1f]
	out[16] = crockfordBase32[(lo>>45)&0x1f]
	out[17] = crockfordBase32[(lo>>40)&0x1f]
	out[18] = crockfordBase32[(lo>>35)&0x1f]
	out[19] = crockfordBase32[(lo>>30)&0x1f]
	out[20] = crockfordBase32[(lo>>25)&0x1f]
	out[21] = crockfordBase32[(lo>>20)&0x1f]
	out[22] = crockfordBase32[(lo>>15)&0x1f]
	out[23] = crockfordBase32[(lo>>10)&0x1f]
	out[24] = crockfordBase32[(lo>>5)&0x1f]
	out[25] = crockfordBase32[lo&0x1f]

	return string(out[:])
}
