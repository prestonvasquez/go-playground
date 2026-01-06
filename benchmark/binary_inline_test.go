package benchmark

import (
	"encoding/binary"
	"testing"
)

func BenchmarkLittleEndianUint32(b *testing.B) {
	b.Run("inline", func(b *testing.B) {
		buf := []byte{0x01, 0x02, 0x03, 0x04}
		b.SetBytes(4)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = inlineLittleEndianUint32(buf)
			}
		})
	})

	b.Run("copy", func(b *testing.B) {
		buf := []byte{0x01, 0x02, 0x03, 0x04}
		b.SetBytes(4)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = copyLittleEndianUint32(buf)
			}
		})
	})
}

func inlineLittleEndianUint32(b []byte) uint32 {
	return binary.LittleEndian.Uint32(b)
}

func copyLittleEndianUint32(b []byte) int32 {
	_ = b[3] // bounds check hint to compiler

	value := int32(b[0]) |
		int32(b[1])<<8 |
		int32(b[2])<<16 |
		int32(b[3])<<24

	return value
}
