//go:build amd64

package wazero

import (
	"github.com/tetratelabs/wazero/wasm"
	"github.com/tetratelabs/wazero/wasm/jit"
)

func getEngine() wasm.Engine {
	return jit.NewEngine()
}
