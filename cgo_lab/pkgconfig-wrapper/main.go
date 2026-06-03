package main

/*
#cgo pkg-config: libmongocrypt
#include <mongocrypt.h>
*/
import "C"
import "fmt"

func main() {
	v := C.mongocrypt_version(nil)
	fmt.Printf("libmongocrypt version: %s\n", C.GoString(v))
}
