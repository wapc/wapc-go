//go:build amd64
// +build amd64

package wasmtime

import (
	"context"
	"log"

	"github.com/bytecodealliance/wasmtime-go"

	"github.com/wapc/wapc-go"
)

// This shows how to customize the underlying wasmer engine used by waPC.
func Example_custom() {
	// Set up the context used to instantiate the engine.
	ctx := context.Background()
	cfg := wasmtime.NewConfig()
	cfg.SetWasmMemory64(true)
	e := EngineWithRuntime(func() (*wasmtime.Engine, error) {
		return wasmtime.NewEngineWithConfig(cfg), nil
	}, false)
	// Configure waPC to use a specific wasmer feature.

	// Instantiate a module normally.
	m, err := e.New(ctx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		log.Panicf("Error creating module - %v\n", err)
	}
	defer m.Close(ctx)

	// Output:
}
