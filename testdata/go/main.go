package main

import (
	"fmt"
	wapc "github.com/wapc/wapc-guest-tinygo"
)

func main() {
	wapc.RegisterFunctions(wapc.Functions{
		"echo": echo,
		"nope": fail,
	})
}

func echo(payload []byte) ([]byte, error) {
	// Callback with Payload
	wapc.HostCall("wapc", "testing", "echo", payload)
	return payload, nil
}

func fail(payload []byte) ([]byte, error) {
	return []byte(""), fmt.Errorf("Planned Failure")
}
