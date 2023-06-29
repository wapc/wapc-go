package wasmer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/wasmerio/wasmer-go/wasmer"

	"github.com/wapc/wapc-go"
)

func sha256FromBytes(guest []byte) string {
	hash := sha256.New()

	// Read the first 64K bytes from the file
	maxbufferSize := 64 * 1024

	if len(guest) < maxbufferSize {
		maxbufferSize = len(guest)
	}

	// Write the read bytes to the hash object
	hash.Write(guest[:maxbufferSize])

	// Get the hash sum as a byte slice
	return hex.EncodeToString(hash.Sum(nil))
}

var cache = func(r *wasmer.Store, b []byte) (*wasmer.Module, error) {
	td := os.TempDir()
	basePath := filepath.Join(td, "wapc-cache", "wapc-wasmer", "1.0.0")
	if err := os.MkdirAll(basePath, 0755); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}
	sha := sha256FromBytes(guest)
	cachePath := filepath.Join(basePath, sha)
	f, err := os.ReadFile(cachePath)
	var module *wasmer.Module
	if err != nil {
		module, err = wasmer.NewModule(r, guest)
		if err != nil {
			return nil, err
		}
		compiled, err := module.Serialize()
		if err != nil {
			return nil, err
		}
		err = os.WriteFile(cachePath, compiled, 0444)
		if err != nil {
			return nil, err
		}
	} else {
		module, err = wasmer.DeserializeModule(r, f)
		if err != nil {
			return nil, err
		}
	}
	return module, nil
}

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

func TestEngine_WithEngine(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		expected := wasmer.NewEngine()

		e := EngineWith(WithRuntime(func() (*wasmer.Engine, error) {
			return expected, nil
		}), WithCaching(cache))

		m, err := e.New(testCtx, wapc.NoOpHostCallHandler, guest, mc)
		if err != nil {
			t.Errorf("Error creating module - %v", err)
		}
		defer m.Close(testCtx)

		if have := m.(*Module).engine; have != expected {
			t.Errorf("Unexpected engine, have %v, expected %v", have, expected)
		}
	})

	t.Run("nil not ok", func(t *testing.T) {
		expectedErr := "function set by WithEngine returned nil"
		e := EngineWith(WithRuntime(func() (*wasmer.Engine, error) {
			return nil, errors.New(expectedErr)
		}), WithCaching(cache))

		if _, err := e.New(testCtx, wapc.NoOpHostCallHandler, guest, mc); err.Error() != expectedErr {
			t.Errorf("Unexpected error, have %v, expected %v", err, expectedErr)
		}
	})
}

func TestModule_Unwrap(t *testing.T) {
	m, err := EngineWith(WithRuntime(DefaultRuntime), WithCaching(cache)).New(testCtx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		t.Errorf("Error creating module - %v", err)
	}
	defer m.Close(testCtx)

	mod := m.(*Module)
	expected := mod.module
	if have := mod.Unwrap(); have != expected {
		t.Errorf("Unexpected module, have %v, expected %v", have, expected)
	}
}

func TestInstance_Unwrap(t *testing.T) {
	m, err := EngineWith(WithRuntime(DefaultRuntime), WithCaching(cache)).New(testCtx, wapc.NoOpHostCallHandler, guest, mc)
	if err != nil {
		t.Errorf("Error creating module - %v", err)
	}
	defer m.Close(testCtx)

	i, err := m.Instantiate(testCtx)
	if err != nil {
		t.Errorf("Error creating instance - %v", err)
	}
	defer m.Close(testCtx)

	inst := i.(*Instance)
	expected := inst.inst
	if have := inst.Unwrap(); have != expected {
		t.Errorf("Unexpected instance, have %v, expected %v", have, expected)
	}
}
