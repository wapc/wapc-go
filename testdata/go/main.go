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
	_, err := wapc.HostCall("wapc", "testing", "echo", payload)
	if err != nil {
		return []byte(""), err
	}
	return payload, nil
}

// Fail will return an error when called
func Fail(payload []byte) ([]byte, error) {
	return []byte(""), fmt.Errorf("Planned Failure")
}
