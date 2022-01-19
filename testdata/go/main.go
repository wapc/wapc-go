package main

import (
	"fmt"
	wapc "github.com/wapc/wapc-guest-tinygo"
)

func main() {
	// Register echo and fail functions
	wapc.RegisterFunctions(wapc.Functions{
		"echo": echo,
		"nope": fail,
	})
}

// echo will callback the host and return the payload
func echo(payload []byte) ([]byte, error) {
	// Callback with Payload
	wapc.HostCall("wapc", "testing", "echo", payload)
	return payload, nil
}

// fail will return an error when called
func fail(payload []byte) ([]byte, error) {
	return []byte(""), fmt.Errorf("Planned Failure")
}
