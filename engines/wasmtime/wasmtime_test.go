package wasmtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/bytecodealliance/wasmtime-go"

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
		expected := wasmtime.NewEngine()

		e := EngineWith(WithRuntime(func() (*wasmtime.Engine, error) {
			return expected, nil
		}))
		m, err := e.New(wapc.WithContext(testCtx), wapc.WithHost(wapc.NoOpHostCallHandler), wapc.WithGuest(guest), wapc.WithConfig(mc))
		if err != nil {
			t.Errorf("Error creating module - %v", err)
		}
		defer m.Close(testCtx)

		if have := m.(*Module).engine; have != expected {
			t.Errorf("Unexpected engine, have %v, expected %v", have, expected)
		}
	})
	t.Run("ok", func(t *testing.T) {
		cfg := wasmtime.NewConfig()
		err := cfg.CacheConfigLoadDefault()
		if err != nil {
			t.Errorf("Error failed to configure cache - %v", err)
		}
		expected := wasmtime.NewEngineWithConfig(cfg)

		e := EngineWith(WithRuntime(func() (*wasmtime.Engine, error) {
			return expected, nil
		}))
		m, err := e.New(wapc.WithContext(testCtx), wapc.WithHost(wapc.NoOpHostCallHandler), wapc.WithGuest(guest), wapc.WithConfig(mc))
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
		e := EngineWith(WithRuntime(func() (*wasmtime.Engine, error) {
			return nil, errors.New(expectedErr)
		}))

		if _, err := e.New(wapc.WithContext(testCtx), wapc.WithHost(wapc.NoOpHostCallHandler), wapc.WithGuest(guest), wapc.WithConfig(mc)); err.Error() != expectedErr {
			t.Errorf("Unexpected error, have %v, expected %v", err, expectedErr)
		}
	})
}

func TestModule_Unwrap(t *testing.T) {
	m, err := EngineWith(WithRuntime(DefaultRuntime)).New(wapc.WithContext(testCtx), wapc.WithHost(wapc.NoOpHostCallHandler), wapc.WithGuest(guest), wapc.WithConfig(mc))
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
	m, err := EngineWith(WithRuntime(DefaultRuntime)).New(wapc.WithContext(testCtx), wapc.WithHost(wapc.NoOpHostCallHandler), wapc.WithGuest(guest), wapc.WithConfig(mc))
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
