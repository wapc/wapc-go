//go:build wasmtime && !wasmer
// +build wasmtime,!wasmer

package main

import (
	"github.com/wapc/wapc-go"
	"github.com/wapc/wapc-go/engines/wasmtime"
)

func getEngine() wapc.Engine {
	return wasmtime.Engine()
}
