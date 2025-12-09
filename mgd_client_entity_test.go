//go:build ceust
// +build ceust

package goplayground

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMGDClientEntity(t *testing.T) {
	var testFile testFile
	var raw bson.Raw
	require.NoError(t, bson.UnmarshalExtJSON(testJSON, false, &raw))
	require.NoError(t, bson.Unmarshal(raw, &testFile))

	// After unmarshaling, file-level entities are merged into each test case.
	// Verify test case has all expected entities (file + test level).
	require.NotEmpty(t, testFile.Tests)
	tc := testFile.Tests[0]

	// File-level clients inherited by test case
	client1, ok := tc.Entities.client("fileClient1")
	require.True(t, ok)
	require.Nil(t, client1.AutoEncryptOpts)

	// File-level client overridden by test-level definition (no autoEncryptOpts)
	client0, ok := tc.Entities.client("fileClient0")
	require.True(t, ok)
	require.Nil(t, client0.AutoEncryptOpts)

	// Test-level clients
	tclient0, ok := tc.Entities.client("testClient0")
	require.True(t, ok)
	require.NotNil(t, tclient0.AutoEncryptOpts)

	tclient1, ok := tc.Entities.client("testClient1")
	require.True(t, ok)
	require.Nil(t, tclient1.AutoEncryptOpts)

	// Test-level database
	tdb0, ok := tc.Entities.database("testDb0")
	require.True(t, ok)
	require.Equal(t, "testClient0", tdb0.Client)
}

type clientEntity struct {
	ID              string   `bson:"id"`
	AutoEncryptOpts bson.Raw `bson:"autoEncryptOpts,omitempty"`

	// add other client fields as needed
}

type databaseEntity struct {
	ID           string `bson:"id"`
	Client       string `bson:"client"`
	DatabaseName string `bson:"databaseName"`
	// add options if you need them
}

// Entities is a logical view of createEntities. The Go Driver has to do this
// since it doesn't support unmarshaling into an array of heterogeneous types.
type Entities struct {
	clients   map[string]clientEntity   `bson:"clients,omitempty"`   // Map of client IDs to client entities
	databases map[string]databaseEntity `bson:"databases,omitempty"` // Map of database IDs to database entities
}

type testCase struct {
	Entities Entities `bson:"createEntities,omitempty"`
}

type testFile struct {
	Tests []testCase `bson:"tests"`
}

// UnmarshalBSON merges file-level client entities into each test case, with the
// test case definitions winning on duplicate IDs.
func (tf *testFile) UnmarshalBSON(data []byte) error {
	// Decode into an alias to avoid infinite recursion on this method.
	type alias struct {
		Entities Entities   `bson:"createEntities"`
		Tests    []testCase `bson:"tests"`
	}
	var tmp alias
	if err := bson.Unmarshal(data, &tmp); err != nil {
		return err
	}
	// For each test, merge file-level clients -> test-level clients (test wins).
	for i := range tmp.Tests {
		merged := make(map[string]clientEntity, len(tmp.Entities.clients)+len(tmp.Tests[i].Entities.clients))
		for k, v := range tmp.Entities.clients {
			merged[k] = v
		}
		for k, v := range tmp.Tests[i].Entities.clients {
			merged[k] = v
		}
		tmp.Tests[i].Entities.clients = merged
	}
	// Assign back (file-level entities no longer stored).
	tf.Tests = tmp.Tests
	return nil
}

func (e *Entities) UnmarshalBSONValue(t byte, data []byte) error {
	// The field is tagged as `bson:"createEntities"`, so the decoder passes the
	// value of that field (a BSON array) here. Decode that array into a list of
	// one-of {client|database} entries and build our lookup maps.
	type rawEntity struct {
		Client   *clientEntity   `bson:"client,omitempty"`
		Database *databaseEntity `bson:"database,omitempty"`
	}

	var list []rawEntity
	// First try to decode the provided value as an array directly.
	if err := bson.UnmarshalValue(bson.Type(t), data, &list); err != nil {
		// Some code paths might pass a document wrapper; handle that too.
		var tmp struct {
			CreateEntities []rawEntity `bson:"createEntities"`
		}
		if err2 := bson.Unmarshal(data, &tmp); err2 != nil {
			return err // return the original error
		}
		list = tmp.CreateEntities
	}

	e.clients = make(map[string]clientEntity)
	e.databases = make(map[string]databaseEntity)

	for _, r := range list {
		switch {
		case r.Client != nil:
			id := r.Client.ID
			if id == "" {
				return fmt.Errorf("client entity missing id")
			}
			if _, dup := e.clients[id]; dup {
				return fmt.Errorf("duplicate client id %q", id)
			}
			e.clients[id] = *r.Client

		case r.Database != nil:
			id := r.Database.ID
			if id == "" {
				return fmt.Errorf("database entity missing id")
			}
			if _, dup := e.databases[id]; dup {
				return fmt.Errorf("duplicate database id %q", id)
			}
			e.databases[id] = *r.Database

		default:
			return fmt.Errorf("entity must have exactly one top-level key")
		}
	}

	return nil
}

func getByID[T any](m map[string]T, id string) (T, bool) {
	ret, ok := m[id]
	if !ok {
		return *new(T), false
	}

	return ret, true
}

func (e *Entities) client(id string) (clientEntity, bool) {
	return getByID(e.clients, id)
}

func (e *Entities) database(id string) (databaseEntity, bool) {
	return getByID(e.databases, id)
}

func TestAutoMergeOnUnmarshal(t *testing.T) {
	var tf testFile
	var raw bson.Raw
	require.NoError(t, bson.UnmarshalExtJSON(testJSON, false, &raw))
	require.NoError(t, bson.Unmarshal(raw, &tf))

	// Ensure inherited clients are present in the test case and overrides apply.
	require.NotEmpty(t, tf.Tests)
	tc := tf.Tests[0]

	// Inherited from file level
	_, ok := tc.Entities.client("fileClient1")
	require.True(t, ok)
	// Overridden fileClient0 from test-level definition
	c0, ok := tc.Entities.client("fileClient0")
	require.True(t, ok)
	require.Nil(t, c0.AutoEncryptOpts)
}

var testJSON = []byte(`{
  "description": "entities decoding with file- and test-level clients + databases",
  "schemaVersion": "1.0",
  "createEntities": [
    {
      "client": {
        "id": "fileClient0",
        "autoEncryptOpts": {
          "keyVaultNamespace": "keyvault.datakeys",
          "kmsProviders": {
            "aws": {
              "accessKeyId": { "$$placeholder": 1 },
              "secretAccessKey": { "$$placeholder": 1 }
            }
          }
        }
      }
    },
    {
      "client": {
        "id": "fileClient1"
      }
    },
    {
      "database": {
        "id": "db0",
        "client": "fileClient0",
        "databaseName": "fileLevelDb"
      }
    }
  ],
  "tests": [
    {
      "description": "test-level entities with own clients and database",
      "createEntities": [
        {
          "client": {
            "id": "testClient0",
            "autoEncryptOpts": {
              "keyVaultNamespace": "keyvault.datakeys",
              "kmsProviders": {
                "aws": {
                  "accessKeyId": "",
                  "secretAccessKey": "",
                  "sessionToken": "$$placeholder"
                }
              }
            }
          }
        },
        {
          "client": {
            "id": "testClient1"
          }
        },
        {
          "client": {
            "id": "fileClient0"
          }
        },
        {
          "database": {
            "id": "testDb0",
            "client": "testClient0",
            "databaseName": "testLevelDb"
          }
        }
      ],
      "operations": []
    }
  ]
}`)
