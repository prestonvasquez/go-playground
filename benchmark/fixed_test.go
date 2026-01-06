package benchmark

import (
	"encoding/binary"
	"testing"
)

// Test 1: Both helpers can inline (current situation)
func inlineHelper(b []byte) uint32 {
	return binary.LittleEndian.Uint32(b)
}

func copyHelper(b []byte) uint32 {
	_ = b[3]
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// Test 2: Force both helpers to NOT inline
//go:noinline
func inlineHelperNoInline(b []byte) uint32 {
	return binary.LittleEndian.Uint32(b)
}

//go:noinline
func copyHelperNoInline(b []byte) uint32 {
	_ = b[3]
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func BenchmarkWithInlining(b *testing.B) {
	b.Run("copy", func(b *testing.B) {
		buf := []byte{0x01, 0x02, 0x03, 0x04}
		b.SetBytes(4)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = copyHelper(buf)
			}
		})
	})
	b.Run("stdlib", func(b *testing.B) {
		buf := []byte{0x01, 0x02, 0x03, 0x04}
		b.SetBytes(4)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = inlineHelper(buf)
			}
		})
	})
}

func BenchmarkNoInlining(b *testing.B) {
	b.Run("copy", func(b *testing.B) {
		buf := []byte{0x01, 0x02, 0x03, 0x04}
		b.SetBytes(4)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = copyHelperNoInline(buf)
			}
		})
	})
	b.Run("stdlib", func(b *testing.B) {
		buf := []byte{0x01, 0x02, 0x03, 0x04}
		b.SetBytes(4)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = inlineHelperNoInline(buf)
			}
		})
	})
}
