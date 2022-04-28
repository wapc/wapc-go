//go:build wasmtime && !wasmer
// +build wasmtime,!wasmer

package wapc_test

import (
	"testing"

	"github.com/JanFalkin/wapc-go"
	"github.com/JanFalkin/wapc-go/engines/wasmtime"
)

var wasmtimeEngine = []wapc.Engine{wasmtime.Engine()}

func TestGuests(t *testing.T) {
	testGuests(t, wasmtimeEngine)
}

func TestModuleBadBytes(t *testing.T) {
	testModuleBadBytes(t, wasmtimeEngine)
}

func TestModule(t *testing.T) {
	testModule(t, wasmtimeEngine)
}
func TestGuestsWithPool(t *testing.T) {
	testGuestsWithPool(t, wasmtimeEngine)
}
