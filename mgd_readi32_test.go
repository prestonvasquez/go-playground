package goplayground

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func read32[T ~uint32 | ~int32](src []byte) (T, []byte, bool) { // Unsafe
	if len(src) < 4 {
		return 0, src, false
	}

	// int32 ~[uint32] -> overflow
	return T(binary.LittleEndian.Uint32(src)), src[4:], true
}

func readi32(src []byte) (int32, []byte, bool) {
	if len(src) < 4 {
		return 0, src, false
	}

	_ = src[3] // bounds check hint to compiler

	value := int32(src[0]) |
		int32(src[1])<<8 |
		int32(src[2])<<16 |
		int32(src[3])<<24

	return value, src[4:], true
}

func TestMGD_read32(t *testing.T) {
	_, src, err := bson.MarshalValue(math.MaxUint32)
	require.NoError(t, err)

	i32, _, ok := read32[int32](src)
	require.True(t, ok)
	require.Equal(t, int32(-1), i32) // overflow
}

func TestMGD_readi32(t *testing.T) {
	_, src, err := bson.MarshalValue(math.MaxUint32)
	require.NoError(t, err)

	i32, _, ok := readi32(src)
	require.True(t, ok)
	require.Equal(t, int32(-1), i32) // overflow
}
