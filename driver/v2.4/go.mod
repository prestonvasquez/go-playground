module github.com/prestonvasquez/v24

go 1.25.0

require (
	github.com/stretchr/testify v1.11.1
	go.mongodb.org/mongo-driver/v2 v2.5.0
)

// V2
//replace go.mongodb.org/mongo-driver/v2 => /Users/preston.vasquez/Developer/mongo-go-driver

// V1
//replace go.mongodb.org/mongo-driver => /Users/preston.vasquez/Developer/mongo-go-driver-2

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
