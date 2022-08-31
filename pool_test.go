package wapc_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wapc/wapc-go"
)

func testGuestsWithPool(t *testing.T, engines []wapc.Engine) {
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
					m, err := engine.New(ctx, host, guest, &wapc.ModuleConfig{})
					if err != nil {
						t.Errorf("Error creating module - %s", err)
					}
					defer m.Close(ctx)

					p, err := wapc.NewPool(ctx, m, 10)
					if err != nil {
						t.Errorf("Error creating module pool - %s", err)
					}
					defer p.Close(ctx)

					i, err := p.Get(10 * time.Second)
					if err != nil {
						t.Errorf("Error unable to fetch instance from pool - %s", err)
					}

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

					err = p.Return(i)
					if err != nil {
						t.Errorf("Unexpected error when returning instance to pool - %s", err)
					}
				})
			}
		})
	}
}
