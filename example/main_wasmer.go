//go:build !wasmtime && wasmer
// +build !wasmtime,wasmer

package main

import (
	"github.com/wapc/wapc-go"
	"github.com/wapc/wapc-go/engines/wasmer"
)

func getEngine() wapc.Engine {
	return wasmer.Engine()
}
