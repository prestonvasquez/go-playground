package goplayground

import (
	"bytes"
	"math"
	"reflect"
	"testing"
	"time"

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
	// Create a fresh registry with driver defaults.
	reg := bson.NewRegistry()

	// Capture the default time.Time decoder BEFORE overriding it.
	baseDec, err := reg.LookupDecoder(tTime)
	if err != nil {
		t.Fatalf("failed to lookup default time.Time decoder: %v", err)
	}

	// Register our wrapper for time.Time that handles doubles while
	// preserving default behavior for all other BSON types.
	reg.RegisterTypeDecoder(tTime, timeDecoderWithDoubleFallback(baseDec))

	// Create a BSON document with a double representing epoch milliseconds
	// (simulating legacy data stored as double).
	epochMs := 1700000000000.0
	doc := bson.D{
		{Key: "timestamp", Value: epochMs},
	}

	encoded, err := bson.Marshal(doc)
	if err != nil {
		t.Fatalf("failed to marshal document: %v", err)
	}

	// Decode into a struct with time.Time field.
	type Document struct {
		Timestamp time.Time `bson:"timestamp"`
	}

	var result Document
	decoder := bson.NewDecoder(bson.NewDocumentReader(bytes.NewReader(encoded)))
	decoder.SetRegistry(reg)

	err = decoder.Decode(&result)
	if err != nil {
		t.Fatalf("failed to decode document: %v", err)
	}

	expectedTime := time.Unix(1700000000, 0).UTC()
	if !result.Timestamp.Equal(expectedTime) {
		t.Errorf("expected time %v, got %v", expectedTime, result.Timestamp)
	}
}
