//go:build wasmtime && !wasmer
// +build wasmtime,!wasmer

package main

import (
	"github.com/JanFalkin/wapc-go"
	"github.com/JanFalkin/wapc-go/engines/wasmtime"
)

func getEngine() wapc.Engine {
	return wasmtime.Engine()
}
