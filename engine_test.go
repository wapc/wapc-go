package wapc_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wapc/wapc-go"
)

var ctx = context.Background()

func testGuests(t *testing.T, engines []wapc.Engine) {
	lang := map[string]string{
		"assemblyscript": "as/hello.wasm",
		"go":             "go/hello.wasm",
		"rust":           "rust/hello.wasm",
	}
	for _, engine := range engines {
		t.Run(engine.Name(), func(t *testing.T) {
			for l, p := range lang {
				t.Run("Module testing with "+l+" Guest", func(t *testing.T) {
					// Read .wasm file
					guest, err := os.ReadFile("testdata/" + p)
					if err != nil {
						t.Errorf("Unable to open test file - %s", err)
					}

					// Use these later
					callbackCh := make(chan struct{}, 2)
					host := func(context.Context, string, string, string, []byte) ([]byte, error) {
						callbackCh <- struct{}{}
						return []byte(""), nil
					}

					payload := []byte("Testing")

					// Create new module with a callback function
					m, err := engine.New(ctx, host, guest, &wapc.ModuleConfig{
						Logger: wapc.PrintlnLogger,
						Stdout: os.Stdout,
						Stderr: os.Stderr,
					})
					if err != nil {
						t.Errorf("Error creating module - %s", err)
					}
					defer m.Close(ctx)

					// Instantiate Module
					i, err := m.Instantiate(ctx)
					if err != nil {
						t.Errorf("Error instantiating module - %s", err)
					}
					defer i.Close(ctx)

					t.Run("Call Successful Function", func(t *testing.T) {
						// Call echo function
						r, err := i.Invoke(ctx, "echo", payload)
						if err != nil {
							t.Errorf("Unexpected error when calling wasm module - %s", err)
						}

						// Verify payload is returned
						if len(r) != len(payload) {
							t.Errorf("Unexpected response message, got %s, expected %s", r, payload)
						}

						// Verify if callback is called
						select {
						case <-time.After(5 * time.Second):
							t.Errorf("Timeout waiting for callback execution")
						case <-callbackCh:
							return
						}
					})

					t.Run("Call Failing Function", func(t *testing.T) {
						// Call nope function
						_, err := i.Invoke(ctx, "nope", payload)
						if err == nil {
							t.Errorf("Expected error when calling failing function, got nil")
						}
					})

					t.Run("Call Unregistered Function", func(t *testing.T) {
						_, err := i.Invoke(ctx, "404", payload)
						if err == nil {
							t.Errorf("Expected error when calling unregistered function, got nil")
						}
					})

				})
			}
		})
	}
}

func testModuleBadBytes(t *testing.T, engines []wapc.Engine) {
	for _, engine := range engines {
		t.Run(engine.Name(), func(t *testing.T) {
			host := wapc.NoOpHostCallHandler
			guest := []byte("Do not do this at home kids")
			_, err := engine.New(ctx, host, guest, &wapc.ModuleConfig{})
			if err == nil {
				t.Errorf("Expected error when creating module with invalid wasm, got nil")
			}
		})
	}
}

func testModule(t *testing.T, engines []wapc.Engine) {
	for _, engine := range engines {
		t.Run(engine.Name(), func(t *testing.T) {
			host := wapc.NoOpHostCallHandler
			// Read .wasm file
			guest, err := os.ReadFile("testdata/as/hello.wasm")
			if err != nil {
				t.Errorf("Unable to open test file - %s", err)
			}

			// Use these later
			payload := []byte("Testing")

			// Create new module with a NoOpCallback function
			m, err := engine.New(ctx, host, guest, &wapc.ModuleConfig{
				Logger: wapc.PrintlnLogger,
				Stdout: os.Stdout,
				Stderr: os.Stderr,
			})
			if err != nil {
				t.Errorf("Error creating module - %s", err)
			}
			defer m.Close(ctx)

			// Instantiate Module
			i, err := m.Instantiate(ctx)
			if err != nil {
				t.Errorf("Error instantiating module - %s", err)
			}
			defer i.Close(ctx)

			t.Run("Check MemorySize", func(t *testing.T) {
				// Verify implementations didn't mistake size in bytes for page count.
				expectedMemorySize := uint32(65536) // 1 page
				if i.MemorySize(ctx) != expectedMemorySize {
					t.Errorf("Unexpected memory size, got %d, expected %d", i.MemorySize(ctx), expectedMemorySize)
				}
			})

			t.Run("Call Function", func(t *testing.T) {
				// Call echo function
				r, err := i.Invoke(ctx, "echo", payload)
				if err != nil {
					t.Errorf("Unexpected error when calling wasm module - %s", err)
				}

				// Verify payload is returned
				if len(r) != len(payload) {
					t.Errorf("Unexpected response message, got %s, expected %s", r, payload)
				}
			})

			i.Close(ctx)

			t.Run("Call Function with Closed Instance", func(t *testing.T) {
				// Call echo function
				_, err := i.Invoke(ctx, "echo", payload)
				if err == nil {
					t.Errorf("Expected error when calling wasm module with closed instance")
				}
			})
		})
	}
}
