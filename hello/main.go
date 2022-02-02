package main

import (
	"fmt"

	wapc "github.com/wapc/wapc-guest-tinygo"
)

func main() {
	// Register echo and fail functions
	wapc.RegisterFunctions(wapc.Functions{
		"hello": hello,
	})
}

// hello will callback the host and return the payload
func hello(payload []byte) ([]byte, error) {
	fmt.Println("hello called")
	// Make a host call to capitalize the name.
	nameBytes, err := wapc.HostCall("", "example", "capitalize", payload)
	if err != nil {
		return nil, err
	}
	// Format the message.
	msg := "Hello, " + string(nameBytes)
	return []byte(msg), nil
}
