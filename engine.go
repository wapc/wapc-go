package wapc

import (
	"context"
	"io"
	"unsafe"
)

type (
	// Logger is the waPC logger for `__console_log` function calls.
	Logger func(msg string)

	// HostCallHandler is a function to invoke to handle when a guest is performing a host call.
	HostCallHandler func(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error)

	Engine interface {
		// Name of the engine. Ex. "wazero"
		Name() string
		// New compiles a new WebAssembly module representing the guest, and
		// configures the host functions it uses.
		//   - host: implements host module functions called by the guest
		//	 - guest: the guest WebAssembly binary (%.wasm) to compile
		//   - config: configures the host and guest.
		New(ctx context.Context, host HostCallHandler, guest []byte, config *ModuleConfig) (Module, error)
	}

	MeteringInfo struct {
		MaxInstructions uint64
		Pfn             unsafe.Pointer
	}

	// ModuleConfig includes parameters to Engine.New.
	//
	// Note: Implementations should copy fields they use instead of storing
	// a reference to this type.
	ModuleConfig struct {
		// Logger is the logger waPC uses for `__console_log` calls
		Logger Logger
		// Stdout is the writer WASI uses for `fd_write` to file descriptor 1.
		Stdout io.Writer
		// Stderr is the writer WASI uses for `fd_write` to file descriptor 2.
		Stderr io.Writer
		// Debug is a flag to indicate if a new instance is for debug wasmtime only
		Debug bool
		// An optional struct for metering that applies to wasmer only for now
		Metering *MeteringInfo
	}

	// Module is a WebAssembly Module.
	Module interface {
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

// compile-time check to ensure NoOpHostCallHandler implements HostCallHandler.
var _ HostCallHandler = NoOpHostCallHandler

// NoOpHostCallHandler is a noop host call handler to use if your host does not
// need to support host calls.
func NoOpHostCallHandler(context.Context, string, string, string, []byte) ([]byte, error) {
	return []byte{}, nil
}

// compile-time check to ensure PrintlnLogger implements Logger.
var _ Logger = PrintlnLogger

// PrintlnLogger will print the supplied message to standard error.
// A newline is appended to the end of the message.
func PrintlnLogger(message string) {
	println(message)
}
