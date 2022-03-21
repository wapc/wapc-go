package wapc

import "context"

type (
	// Logger is the function to call from consoleLog inside a waPC module.
	Logger func(msg string)

	// HostCallHandler is a function to invoke to handle when a guest is performing a host call.
	HostCallHandler func(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error)

	Engine interface {
		Name() string
		New(code []byte, hostCallHandler HostCallHandler) (Module, error)
	}

	Module interface {
		SetLogger(logger Logger)
		SetWriter(writer Logger)
		Instantiate() (Instance, error)
		Close()
	}

	Instance interface {
		MemorySize() uint32
		Invoke(ctx context.Context, operation string, payload []byte) ([]byte, error)
		Close()
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
