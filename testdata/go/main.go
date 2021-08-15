package main

import (
	"fmt"
	wapc "github.com/wapc/wapc-guest-tinygo"
)

func main() {
	wapc.RegisterFunctions(wapc.Functions{
		"hello": hello,
	})
}

func hello(payload []byte) ([]byte, error) {
	wapc.HostCall("myBinding", "sample", "hello", []byte("Simon"))
	greeting := fmt.Sprintf("Hello, %s", payload)
	return []byte(greeting), nil
}
