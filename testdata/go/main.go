package main

import (
	"fmt"

	wapc "github.com/wapc/wapc-guest-tinygo"
)

//go:wasmexport wapc_init
func Initialize() {
	// Register echo and fail functions
	wapc.RegisterFunctions(wapc.Functions{
		"echo": Echo,
		"nope": Fail,
	})
}

// Echo will callback the host and return the payload
func Echo(payload []byte) ([]byte, error) {
	// Callback with Payload
	wapc.HostCall("wapc", "testing", "echo", payload)
	return payload, nil
}

// Fail will return an error when called
func Fail(payload []byte) ([]byte, error) {
	return []byte(""), fmt.Errorf("Planned Failure")
}
