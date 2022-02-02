//go:build !amd64

package wazero

import (
	"github.com/tetratelabs/wazero/wasm"
	"github.com/tetratelabs/wazero/wasm/interpreter"
)

func getEngine() wasm.Engine {
	return interpreter.NewEngine()
}
