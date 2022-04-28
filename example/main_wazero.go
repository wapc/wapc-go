//go:build !wasmtime && !wasmer
// +build !wasmtime,!wasmer

package main

import (
	"github.com/wapc/wapc-go"
	"github.com/wapc/wapc-go/engines/wazero"
)

func getEngine() wapc.Engine {
	return wazero.Engine()
}
