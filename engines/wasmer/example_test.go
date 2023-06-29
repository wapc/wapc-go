package wasmer

import (
	"context"
	"log"

	"github.com/wasmerio/wasmer-go/wasmer"

	"github.com/wapc/wapc-go"
)

// This shows how to customize the underlying wasmer engine used by waPC.
func Example_custom() {
	// Set up the context used to instantiate the engine.
	ctx := context.Background()

	// Configure waPC to use a specific wasmer feature.
	e := EngineWith(WithRuntime(func() (*wasmer.Engine, error) {
		return wasmer.NewEngineWithConfig(wasmer.NewConfig().UseDylibEngine()), nil
	}))

	// Instantiate a module normally.
	m, err := e.New(ctx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		log.Panicf("Error creating module - %v\n", err)
	}
	defer m.Close(ctx)

	// Output:
}
