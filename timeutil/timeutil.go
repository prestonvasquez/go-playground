package timeutil

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// BSONTimestampFromTime creates a bson.Timestamp from a time.Time and an
// increment value.
func BSONTimestampFromTime(t time.Time, i uint32) bson.Timestamp {
	t = t.UTC()

	return bson.Timestamp{
		T: uint32(t.Unix()), // seconds since 1970-01-01T00:00:00Z
		I: i,                // caller-provided increment
	}
}
