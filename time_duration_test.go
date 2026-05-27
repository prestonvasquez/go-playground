package goplayground

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
	"time"
)

// SecureFloat64 generates a cryptographically secure float64 in [0.0, 1.0)
func SecureFloat64() (float64, error) {
	// A float64 has 53 bits of precision in its mantissa
	maxLimit := new(big.Int).Lsh(big.NewInt(1), 53)

	// Generate a secure random big.Int between 0 and 2^53 - 1
	bgInt, err := rand.Int(rand.Reader, maxLimit)
	if err != nil {
		return 0, err
	}

	// Convert to float64 and divide by 2^53
	return float64(bgInt.Int64()) / (1 << 53), nil
}

func JitterDuration(d time.Duration) time.Duration {
	f, _ := SecureFloat64()

	return time.Duration(f * float64(d))
}

func TestTime_JitterDuration(t *testing.T) {
	fmt.Println(JitterDuration(10 * time.Second))
}
