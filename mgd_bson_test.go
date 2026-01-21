package goplayground

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var tTime = reflect.TypeOf(time.Time{})

// timeDecoderWithDoubleFallback wraps the default time.Time decoder to support
// decoding BSON double values (interpreted as epoch milliseconds) while
// preserving all other default decoding behaviors.
func timeDecoderWithDoubleFallback(base bson.ValueDecoder) bson.ValueDecoder {
	return bson.ValueDecoderFunc(func(dc bson.DecodeContext, vr bson.ValueReader, val reflect.Value) error {
		if !val.CanSet() || val.Type() != tTime {
			return bson.ValueDecoderError{Name: "TimeDecodeValue", Types: []reflect.Type{tTime}, Received: val}
		}

		if vr.Type() == bson.TypeDouble {
			f64, err := vr.ReadDouble()
			if err != nil {
				return err
			}

			// Interpret as epoch milliseconds (truncate fractional part if present).
			ms := int64(math.Trunc(f64))
			tm := time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond))

			val.Set(reflect.ValueOf(tm))

			return nil
		}

		// Defer to the default time decoder for all other types.
		return base.DecodeValue(dc, vr, val)
	})
}

func TestTimeDecoderWithDoubleFallback(t *testing.T) {
	// How do I register a custom decoder for time.Time in the MongoDB Go driver's
	// BSON registry?

	// Create a fresh registry with driver defaults.
	reg := bson.NewRegistry()

	// Capture the default time.Time decoder BEFORE overriding it.
	baseDec, err := reg.LookupDecoder(tTime)
	require.NoError(t, err, "failed to lookup default time.Time decoder")

	// Register our wrapper for time.Time that handles doubles while
	// preserving default behavior for all other BSON types.
	reg.RegisterTypeDecoder(tTime, timeDecoderWithDoubleFallback(baseDec))

	// Create a BSON document with a double representing epoch milliseconds
	// (simulating legacy data stored as double).
	epochMs := 1700000000000.0
	doc := struct {
		Timestamp float64 `bson:"timestamp"`
	}{Timestamp: epochMs}

	encoded, err := bson.Marshal(doc)
	require.NoError(t, err, "failed to marshal document")

	// Decode into a struct with time.Time field.
	type Document struct {
		Timestamp time.Time `bson:"timestamp"`
	}

	var result Document
	decoder := bson.NewDecoder(bson.NewDocumentReader(bytes.NewReader(encoded)))
	decoder.SetRegistry(reg)

	err = decoder.Decode(&result)
	require.NoError(t, err, "failed to decode document")

	expectedTime := time.Unix(1700000000, 0).UTC()
	if !result.Timestamp.Equal(expectedTime) {
		t.Errorf("expected time %v, got %v", expectedTime, result.Timestamp)
	}
}

func TestMGD_IntToRaw(t *testing.T) {
	t.Run("bson package with raw", func(t *testing.T) {
		doc := bson.D{{Key: "foo", Value: 123123}}

		raw, err := bson.Marshal(doc)
		require.NoError(t, err)

		type RT struct {
			Foo bson.Raw
		}

		rt := RT{}
		err = bson.Unmarshal(raw, &rt)
		require.NoError(t, err)

		t.Logf("Hello, World! (err: %v; rt: %+v)", err, rt)
	})

	t.Run("bson package with string", func(t *testing.T) {
		doc := bson.D{{Key: "foo", Value: 123123}}

		raw, err := bson.Marshal(doc)
		require.NoError(t, err)

		type RT struct {
			Foo string
		}

		require.Error(t, bson.Unmarshal(raw, &RT{}))
	})

	t.Run("json package", func(t *testing.T) {
		jsonData := []byte(`{"foo": 123123}`)

		type RT struct {
			Foo string
		}

		require.Error(t, json.Unmarshal(jsonData, &RT{}))
	})
}

// TestDecimal128_NonCanonicalValidation shows how to detect non-canonical IEEE 754
// decimal128 bytes that exceed BSON's 34-digit precision limit.
//
// Related to NODE-6901: non-canonical bytes produce strings with 35+ digits that
// ParseDecimal128 rejects.
func TestDecimal128_NonCanonicalValidation(t *testing.T) {
	// Validation: round-trip through string. If ParseDecimal128 rejects it,
	// the bytes were non-canonical (more than 34 significant digits).
	validateDecimal128 := func(d bson.Decimal128) error {
		_, err := bson.ParseDecimal128(d.String())
		return err
	}

	// Non-canonical bytes from NODE-6901 (35 digits of precision)
	// These are valid IEEE 754 decimal128, but NOT valid BSON Decimal128.
	nonCanonicalHigh := uint64(0x3003ed4f2a22e639)
	nonCanonicalLow := uint64(0xc4880c61fc34d7b2)
	nonCanonical := bson.NewDecimal128(nonCanonicalHigh, nonCanonicalLow)

	t.Logf("Non-canonical string: %s", nonCanonical.String())

	err := validateDecimal128(nonCanonical)
	require.Error(t, err, "non-canonical bytes should fail validation")
	t.Logf("Validation caught non-canonical bytes: %v", err)

	// Canonical bytes for same-ish value (34 digits)
	canonical, err := bson.ParseDecimal128("1000.55")
	require.NoError(t, err)
	require.NoError(t, validateDecimal128(canonical), "canonical should pass")
}
