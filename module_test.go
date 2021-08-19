package wapc

import (
	"context"
	"io/ioutil"
	"testing"
	"time"
)

func TestGuests(t *testing.T) {
	lang := map[string]string{
		"assemblyscript": "as/hello.wasm",
		"go":             "go/hello.wasm",
	}

	for l, p := range lang {
		t.Run("Module testing with "+l+" Guest", func(t *testing.T) {
			// Read .wasm file
			b, err := ioutil.ReadFile("testdata/" + p)
			if err != nil {
				t.Errorf("Unable to open test file - %s", err)
			}

			// Use these later
			callbackCh := make(chan struct{}, 2)
			payload := []byte("Testing")

			// Create new module with a callback function
			m, err := New(b, func(context.Context, string, string, string, []byte) ([]byte, error) {
				callbackCh <- struct{}{}
				return []byte(""), nil
			})
			if err != nil {
				t.Errorf("Error creating module - %s", err)
			}
			defer m.Close()

			// Set loggers and writers
			m.SetLogger(Println)
			m.SetWriter(Print)

			// Instantiate Module
			i, err := m.Instantiate()
			if err != nil {
				t.Errorf("Error intantiating module - %s", err)
			}
			defer i.Close()

			t.Run("Call Successful Function", func(t *testing.T) {
				// Call echo function
				r, err := i.Invoke(context.Background(), "echo", payload)
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
				_, err := i.Invoke(context.Background(), "nope", payload)
				if err == nil {
					t.Errorf("Expected error when calling failing function, got nil")
				}
			})

			t.Run("Call Unregistered Function", func(t *testing.T) {
				_, err := i.Invoke(context.Background(), "404", payload)
				if err == nil {
					t.Errorf("Expected error when calling unregistered function, got nil")
				}
			})

		})
	}
}

func TestModuleBadBytes(t *testing.T) {
	b := []byte("Do not do this at home kids")
	_, err := New(b, NoOpHostCallHandler)
	if err == nil {
		t.Errorf("Expected error when creating module with invalid wasm, got nil")
	}
}

func TestModule(t *testing.T) {
	// Read .wasm file
	b, err := ioutil.ReadFile("testdata/as/hello.wasm")
	if err != nil {
		t.Errorf("Unable to open test file - %s", err)
	}

	// Use these later
	payload := []byte("Testing")

	// Create new module with a NoOpCallback function
	m, err := New(b, NoOpHostCallHandler)
	if err != nil {
		t.Errorf("Error creating module - %s", err)
	}
	defer m.Close()

	// Set loggers and writers
	m.SetLogger(Println)
	m.SetWriter(Print)

	// Instantiate Module
	i, err := m.Instantiate()
	if err != nil {
		t.Errorf("Error intantiating module - %s", err)
	}
	defer i.Close()

	t.Run("Check MemorySize", func(t *testing.T) {
		_ = i.MemorySize()
	})

	t.Run("Call Function", func(t *testing.T) {
		// Call echo function
		r, err := i.Invoke(context.Background(), "echo", payload)
		if err != nil {
			t.Errorf("Unexpected error when calling wasm module - %s", err)
		}

		// Verify payload is returned
		if len(r) != len(payload) {
			t.Errorf("Unexpected response message, got %s, expected %s", r, payload)
		}
	})

	i.Close()

	t.Run("Call Function with Closed Instance", func(t *testing.T) {
		// Call echo function
		_, err := i.Invoke(context.Background(), "echo", payload)
		if err == nil {
			t.Errorf("Expected error when calling wasm module with closed instance")
		}
	})

}
