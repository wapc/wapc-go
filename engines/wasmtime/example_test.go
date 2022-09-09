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

	// Configure waPC to use a specific wasmer feature.
	e := Engine(WithEngine(func() *wasmtime.Engine {
		return wasmtime.NewEngineWithConfig(func() *wasmtime.Config {
			cfg := wasmtime.NewConfig()
			cfg.SetWasmMemory64(true)
			return cfg
		}())
	}))

	// Instantiate a module normally.
	m, err := e.New(ctx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		log.Panicf("Error creating module - %v\n", err)
	}
	defer m.Close(ctx)

	// Output:
}
