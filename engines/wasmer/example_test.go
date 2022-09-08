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
	var i interface{}

	// Configure waPC to use a specific wasmer feature.
	e := Engine(WithEngine(func(interface{}) *wasmer.Engine {
		return wasmer.NewEngineWithConfig(wasmer.NewConfig().UseDylibEngine())
	}, i))

	// Instantiate a module normally.
	m, err := e.New(ctx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		log.Panicf("Error creating module - %v\n", err)
	}
	defer m.Close(ctx)

	// Output:
}
