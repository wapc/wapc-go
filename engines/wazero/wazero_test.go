package wazero

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	wapc "github.com/wapc/wapc-go"
)

// testCtx is an arbitrary, non-default context. Non-nil also prevents linter errors.
var testCtx = context.WithValue(context.Background(), struct{}{}, "arbitrary")

var guest []byte
var mc = &wapc.ModuleConfig{
	Logger: wapc.PrintlnLogger,
	Stdout: os.Stdout,
	Stderr: os.Stderr,
}

// TestMain ensures we can read the example wasm prior to running unit tests.
func TestMain(m *testing.M) {
	var err error
	guest, err = os.ReadFile("../../testdata/go/hello.wasm")
	if err != nil {
		log.Panicln(err)
	}
	os.Exit(m.Run())
}

func TestEngineWithRuntime(t *testing.T) {
	t.Run("instantiates custom runtime", func(t *testing.T) {
		rc := wazero.NewRuntimeConfig().WithWasmCore2()
		r := wazero.NewRuntimeWithConfig(testCtx, rc)
		defer r.Close(testCtx)

		if _, err := wasi_snapshot_preview1.Instantiate(testCtx, r); err != nil {
			_ = r.Close(testCtx)
			if err != nil {
				t.Errorf("Error creating module - %v", err)
			}
		}

		// TinyGo doesn't need the AssemblyScript host functions which are
		// instantiated by default.
		e := EngineWithRuntime(func(ctx context.Context) (wazero.Runtime, error) {
			return r, nil
		})

		m, err := e.New(testCtx, wapc.NoOpHostCallHandler, guest, mc)
		if err != nil {
			t.Errorf("Error creating module - %v", err)
		}

		if have := m.(*Module).runtime; have != r {
			t.Errorf("Unexpected runtime, got %v, expected %v", have, r)
		}

		// We expect this to close the runtime returned by NewRuntime
		m.Close(testCtx)
		if _, err = r.InstantiateModuleFromBinary(testCtx, guest); err == nil {
			t.Errorf("Expected Module.Close to close wazero Runtime")
		}
	})

	t.Run("error instantiating runtime", func(t *testing.T) {
		expectedErr := errors.New("broken")

		e := EngineWithRuntime(func(context.Context) (wazero.Runtime, error) {
			return nil, expectedErr
		})

		if _, err := e.New(testCtx, wapc.NoOpHostCallHandler, guest, mc); err != expectedErr {
			t.Errorf("Unexpected error, got %v, expected %v", err, expectedErr)
		}
	})
}
