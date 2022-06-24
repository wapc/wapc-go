package wapc

import (
	"context"
	"unsafe"
)

type (
	// Logger is the function to call from consoleLog inside a waPC module.
	Logger func(msg string)

	// HostCallHandler is a function to invoke to handle when a guest is performing a host call.
	HostCallHandler func(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error)

	Engine interface {
		Name() string
		New(ctx context.Context, code []byte, hostCallHandler HostCallHandler) (Module, error)
		NewWithMetering(code []byte, hostCallHandler HostCallHandler, maxInstructions uint64, pfn unsafe.Pointer) (Module, error)
		NewWithDebug(code []byte, hostCallHandler HostCallHandler) (Module, error)
	}

	// Module is a WebAssembly Module.
	Module interface {
		// SetLogger sets the waPC logger for `__console_log` function calls.
		SetLogger(Logger)

		// SetWriter sets the writer for WASI `fd_write` calls to stdout (file descriptor 1).
		SetWriter(Logger)

		// Instantiate creates a single instance of the module with its own memory.
		Instantiate(context.Context) (Instance, error)

		// Close releases resources from this module, returning the first error encountered.
		// Note: This should be called before after calling Instance.Close on any instances of this module.
		Close(context.Context) error
	}

	// Instance is an instantiated Module
	Instance interface {
		// MemorySize is the size in bytes of the memory available to this Instance.
		MemorySize(context.Context) uint32

		// Invoke calls `operation` with `payload` on the module and returns a byte slice payload.
		Invoke(ctx context.Context, operation string, payload []byte) ([]byte, error)

		// Close releases resources from this instance, returning the first error encountered.
		// Note: This should be called before calling Module.Close.

		RemainingPoints(context.Context) uint64
		Close(context.Context) error
	}
)

// NoOpHostCallHandler is an noop host call handler to use if your host does not need to support host calls.
func NoOpHostCallHandler(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
	return []byte{}, nil
}

// Println will print the supplied message to standard error. Newline is appended to the end of the message.
func Println(message string) {
	println(message)
}

// Print will print the supplied message to standard error.
func Print(message string) {
	print(message)
}
