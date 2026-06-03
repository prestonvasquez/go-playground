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

// You can't decode into a map[string]any directly. You have to decode into a
// container and then assign to the map. This is because the decoder needs
// to asssign the address of the map value, but map values are not addressable
// in Go.
func TestMGD_BSON_UnmarshalDirectlyIntoMap(t *testing.T) {
	m := map[string]any{"foo": 123123}

	bytes, err := bson.MarshalExtJSON(m, true, false)
	require.NoError(t, err)

	mapOfMaps := map[string]map[string]any{}

	// Map values are not addressable, so this fails:
	err = bson.UnmarshalExtJSON(bytes, true, mapOfMaps["fooMap"])
	require.Error(t, err)
}

// TestMGD_BSON_ZeroLengthString is the Go analog of js-bson issue #1
// (onDemand.parseToElements non-terminating scan on zero-length string).
//
// In js-bson, findNull loops past the buffer end when a string element
// declares size 0 because it lacks a `< bytes.length` bound. The Go-driver
// analog is whether bsoncore detects size 0 before indexing string content.
//
// BSON strings carry an int32 size that includes the null terminator, so the
// minimum valid size is 1. Size 0 means no bytes at all — not even a
// terminator — and must be rejected rather than cause a panic.
func TestMGD_BSON_ZeroLengthString(t *testing.T) {
	// 12-byte document: outer length is consistent, string element size = 0.
	//   0x0C 0x00 0x00 0x00  total length = 12
	//   0x02                  element type: string
	//   0x61 0x00             key: "a\x00"
	//   0x00 0x00 0x00 0x00  string size = 0  (invalid; minimum is 1)
	//   0x00                  document terminator
	raw := bson.Raw{
		0x0C, 0x00, 0x00, 0x00,
		0x02,
		0x61, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00,
	}

	t.Run("Validate", func(t *testing.T) {
		var (
			err       error
			panicked  bool
			recovered any
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					recovered = r
				}
			}()
			err = raw.Validate()
		}()
		require.Falsef(t, panicked,
			"bson.Raw.Validate panicked on zero-length string element: %v", recovered)
		require.Error(t, err,
			"bson.Raw.Validate should return an error for string size 0")
	})

	t.Run("Unmarshal", func(t *testing.T) {
		var (
			out       bson.M
			err       error
			panicked  bool
			recovered any
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					recovered = r
				}
			}()
			err = bson.Unmarshal(raw, &out)
		}()
		require.Falsef(t, panicked,
			"bson.Unmarshal panicked on zero-length string element: %v", recovered)
		require.Error(t, err,
			"bson.Unmarshal should return an error for string size 0")
	})
}

// TestMGD_BSON_ZeroLengthVectorBinary is the Go analog of js-bson issue #2
// (validateBinaryVector allows a zero-length subtype-9 Binary to serialize).
//
// BSON Binary subtype 9 (Vector) requires at least 2 metadata bytes:
// byte 0 = dtype (element type of the vector), byte 1 = padding count.
// A zero-length payload has neither, producing a semantically invalid vector.
//
// In js-bson, validateBinaryVector reads buffer[0]/buffer[1] without a
// length guard, both return undefined, every dtype branch is skipped, and
// validation passes silently. The Go driver does not define Vector-specific
// constructor helpers, so this test probes whether Validate/Unmarshal treat
// a structurally valid but semantically empty subtype-9 Binary as an error.
func TestMGD_BSON_ZeroLengthVectorBinary(t *testing.T) {
	// 13-byte document: Binary element, subtype 9 (Vector), zero data bytes.
	//   0x0D 0x00 0x00 0x00  total length = 13
	//   0x05                  element type: Binary
	//   0x76 0x00             key: "v\x00"
	//   0x00 0x00 0x00 0x00  binary data length = 0
	//   0x09                  binary subtype = 9 (Vector)
	//   0x00                  document terminator
	raw := bson.Raw{
		0x0D, 0x00, 0x00, 0x00,
		0x05,
		0x76, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x09,
		0x00,
	}

	t.Run("Validate", func(t *testing.T) {
		var (
			err       error
			panicked  bool
			recovered any
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					recovered = r
				}
			}()
			err = raw.Validate()
		}()
		require.Falsef(t, panicked,
			"bson.Raw.Validate panicked on zero-length Vector Binary: %v", recovered)
		// A zero-length Binary is structurally valid BSON; the Vector metadata
		// constraint is semantic. The driver may or may not enforce it here.
		t.Logf("Validate result for zero-length subtype-9 Binary: err=%v", err)
	})

	t.Run("Unmarshal", func(t *testing.T) {
		var (
			out       bson.M
			err       error
			panicked  bool
			recovered any
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					recovered = r
				}
			}()
			err = bson.Unmarshal(raw, &out)
		}()
		require.Falsef(t, panicked,
			"bson.Unmarshal panicked on zero-length Vector Binary: %v", recovered)
		t.Logf("Unmarshal result for zero-length subtype-9 Binary: err=%v out=%+v", err, out)
	})
}

func TestMGD_BSON_Validate_ZeroLength(t *testing.T) {
	// Step 1: Construct a malformed raw BSON buffer whose first four bytes
	// encode an int32 length of zero. This satisfies the minimum length to
	// read the leading int32, but a valid BSON document must be at least 5
	// bytes (4-byte length + trailing 0x00). A zero declared length should
	// be rejected by a validator, not turned into a process panic.
	raw := bson.Raw{0x00, 0x00, 0x00, 0x00}

	// Step 2: Call Validate inside a deferred recover so the test can
	// distinguish a panic (the bug) from a returned validation error (the
	// correct behavior). Without the recover, the bug causes the test
	// process to crash before any assertion can run.
	var (
		err       error
		panicked  bool
		recovered any
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				recovered = r
			}
		}()
		err = raw.Validate()
	}()

	// Step 3: Assert that Validate did not panic. On the buggy driver this
	// assertion fails because bsoncore.Document.Validate indexes d[length-1]
	// with length == 0, producing a runtime index-out-of-range panic.
	require.Falsef(t, panicked,
		"bson.Raw.Validate panicked on malformed zero-length document: %v",
		recovered)

	// Step 4: Assert that Validate returned a non-nil error. A four-byte
	// buffer with a declared length of zero is not a valid BSON document
	// and must be reported as a validation error rather than silently
	// accepted or crashing the process.
	require.Error(t, err,
		"bson.Raw.Validate should return an error for a zero-length document")
}
