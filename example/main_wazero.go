//go:build !wasmtime && !wasmer
// +build !wasmtime,!wasmer

package main

import (
	"github.com/JanFalkin/wapc-go"
	"github.com/JanFalkin/wapc-go/engines/wazero"
)

func getEngine() wapc.Engine {
	return wazero.Engine()
}
