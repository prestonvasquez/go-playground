package benchmark

import (
	"encoding/binary"
	"testing"
)

// Master approach: stdlib + cast
func masterApproach(src []byte) (int32, []byte, bool) {
	if len(src) < 4 {
		return 0, src, false
	}
	return int32(binary.LittleEndian.Uint32(src)), src[4:], true
}

// PR approach: manual bit shifting
func prApproach(src []byte) (int32, []byte, bool) {
	if len(src) < 4 {
		return 0, src, false
	}
	_ = src[3]
	value := int32(src[0]) |
		int32(src[1])<<8 |
		int32(src[2])<<16 |
		int32(src[3])<<24
	return value, src[4:], true
}

func BenchmarkMasterApproach(b *testing.B) {
	buf := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	b.SetBytes(4)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = masterApproach(buf)
		}
	})
}

func BenchmarkPRApproach(b *testing.B) {
	buf := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	b.SetBytes(4)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = prApproach(buf)
		}
	})
}
