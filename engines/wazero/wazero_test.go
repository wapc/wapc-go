package wazero

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/wapc/wapc-go"
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

// TestModule_UnwrapRuntime ensures the Unwrap returns the correct Runtime interface
func TestModule_UnwrapRuntime(t *testing.T) {
	m, err := EngineWithRuntime(DefaultRuntime).New(testCtx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		t.Errorf("Error creating module - %v", err)
	}
	defer m.Close(testCtx)

	mod := m.(*Module)
	expected := &mod.runtime
	if have := mod.UnwrapRuntime(); have != expected {
		t.Errorf("Unexpected module, have %v, expected %v", have, expected)
	}
}

// TestModule_WithConfig ensures the module config can be extended
func TestModule_WithConfig(t *testing.T) {
	m, err := EngineWithRuntime(DefaultRuntime).New(testCtx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		t.Errorf("Error creating module - %v", err)
	}
	defer m.Close(testCtx)

	var mock = &mockModuleConfig{}
	m.(*Module).config = mock
	m.(*Module).WithConfig(func(config wazero.ModuleConfig) wazero.ModuleConfig {
		return config.WithSysWalltime()
	})
	if !mock.calledWithSysWalltime {
		t.Errorf(`Expected call to WithSysWalltime`)
	}
}

type mockModuleConfig struct {
	wazero.ModuleConfig
	calledWithSysWalltime bool
}

func (m *mockModuleConfig) WithSysWalltime() wazero.ModuleConfig {
	m.calledWithSysWalltime = true
	return m
}

// TestInstance_UnwrapModule ensures the Unwrap returns the correct api.Module interface
func TestInstance_UnwrapModule(t *testing.T) {
	m, err := EngineWithRuntime(DefaultRuntime).New(testCtx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		t.Errorf("Error creating module - %v", err)
	}
	defer m.Close(testCtx)

	mod, err := m.Instantiate(testCtx)
	if err != nil {
		t.Errorf("Error instantiating module - %v", err)
	}
	inst := mod.(*Instance)
	expected := inst.m
	if have := inst.UnwrapModule(); have != expected {
		t.Errorf("Unexpected module, have %v, expected %v", have, expected)
	}
}

func TestEngineWithRuntime(t *testing.T) {
	t.Run("instantiates custom runtime", func(t *testing.T) {
		r := wazero.NewRuntime(testCtx)
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
		// Ensure the runtime is now closed by invoking a related method
		if mod := r.Module(wasi_snapshot_preview1.ModuleName); mod != nil {
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
