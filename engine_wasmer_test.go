//go:build !wasmtime && wasmer
// +build !wasmtime,wasmer

package wapc_test

import (
	"testing"

	"github.com/wapc/wapc-go"
	"github.com/wapc/wapc-go/engines/wasmer"
)

var wasmerEngine = []wapc.Engine{wasmer.Engine()}

func TestGuests(t *testing.T) {
	testGuests(t, wasmerEngine)
}

func TestModuleBadBytes(t *testing.T) {
	testModuleBadBytes(t, wasmerEngine)
}

func TestModule(t *testing.T) {
	testModule(t, wasmerEngine)
}

func TestGuestsWithPool(t *testing.T) {
	testGuestsWithPool(t, wasmerEngine)
}
