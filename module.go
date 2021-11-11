package wapc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/wasmerio/wasmer-go/wasmer"
)

type (
	// Logger is the function to call from consoleLog inside a waPC module.
	Logger func(msg string)

	// HostCallHandler is a function to invoke to handle when a guest is performing a host call.
	HostCallHandler func(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error)

	// Module represents a compile waPC module.
	Module struct {
		logger Logger // Logger to use for waPC's __console_log
		writer Logger // Logger to use for WASI fd_write (where fd == 1 for standard out)

		hostCallHandler HostCallHandler

		engine *wasmer.Engine
		store  *wasmer.Store
		module *wasmer.Module
	}

	// Instance is a single instantiation of a module with its own memory.
	Instance struct {
		m         *Module
		guestCall func(...interface{}) (interface{}, error)

		inst *wasmer.Instance
		mem  *wasmer.Memory

		context *invokeContext

		// external functions
		guestRequest    *wasmer.Function
		guestResponse   *wasmer.Function
		guestError      *wasmer.Function
		hostCall        *wasmer.Function
		hostResponseLen *wasmer.Function
		hostResponse    *wasmer.Function
		hostErrorLen    *wasmer.Function
		hostError       *wasmer.Function
		consoleLog      *wasmer.Function
		abort           *wasmer.Function
		fdWrite         *wasmer.Function
		argsSizesGet    *wasmer.Function
		argsGet         *wasmer.Function
	}

	invokeContext struct {
		ctx       context.Context
		operation string

		guestReq  []byte
		guestResp []byte
		guestErr  string

		hostResp []byte
		hostErr  error
	}
)

// NoOpHostCallHandler is an noop host call handler to use if your host does not need to support host calls.
func NoOpHostCallHandler(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
	return []byte{}, nil
}

// New compiles a `Module` from `code`.
func New(code []byte, hostCallHandler HostCallHandler) (*Module, error) {
	engine := wasmer.NewEngine()
	store := wasmer.NewStore(engine)

	module, err := wasmer.NewModule(store, code)
	if err != nil {
		return nil, err
	}

	return &Module{
		engine:          engine,
		store:           store,
		module:          module,
		hostCallHandler: hostCallHandler,
	}, nil
}

// SetLogger sets the waPC logger for __console_log calls.
func (m *Module) SetLogger(logger Logger) {
	m.logger = logger
}

// SetWriter sets the writer for WASI fd_write calls to standard out.
func (m *Module) SetWriter(writer Logger) {
	m.writer = writer
}

// Instantiate creates a single instance of the module with its own memory.
func (m *Module) Instantiate() (*Instance, error) {
	instance := Instance{
		m: m,
	}
	importObject := wasmer.NewImportObject()
	importObject.Register("env", instance.envRuntime())
	importObject.Register("wapc", instance.wapcRuntime())
	wasiRuntime := instance.wasiRuntime()
	importObject.Register("wasi_unstable", wasiRuntime)
	importObject.Register("wasi_snapshot_preview1", wasiRuntime)
	importObject.Register("wasi", wasiRuntime)

	inst, err := wasmer.NewInstance(m.module, importObject)
	if err != nil {
		return nil, err
	}
	instance.inst = inst

	instance.mem, err = inst.Exports.GetMemory("memory")
	if err != nil {
		return nil, err
	}

	instance.guestCall, err = inst.Exports.GetFunction("__guest_call")
	if err != nil {
		return nil, errors.New("could not find exported function '__guest_call'")
	}

	// Initialize the instance of it exposes a `_start` function.
	initFunctions := []string{"_start", "wapc_init"}
	for _, initFunction := range initFunctions {
		if initFn, err := inst.Exports.GetFunction(initFunction); err == nil {
			context := invokeContext{
				ctx: context.Background(),
			}
			instance.context = &context

			if _, err := initFn(); err != nil {
				return nil, fmt.Errorf("could not initialize instance: %w", err)
			}
		}
	}

	return &instance, nil
}

func (i *Instance) envRuntime() map[string]wasmer.IntoExtern {
	// abort is for assemblyscript
	i.abort = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(
			wasmer.NewValueTypes(wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32),
			wasmer.NewValueTypes()),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			return []wasmer.Value{}, nil
		},
	)
	return map[string]wasmer.IntoExtern{
		"abort": i.abort,
	}
}

func (i *Instance) wapcRuntime() map[string]wasmer.IntoExtern {
	i.guestRequest = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes()),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			operationPtr := args[0].I32()
			payloadPtr := args[1].I32()
			data := i.mem.Data()
			copy(data[operationPtr:], i.context.operation)
			copy(data[payloadPtr:], i.context.guestReq)
			return []wasmer.Value{}, nil
		},
	)
	i.guestResponse = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes()),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			ptr := args[0].I32()
			len := args[1].I32()
			data := i.mem.Data()
			buf := make([]byte, len)
			copy(buf, data[ptr:ptr+len])
			i.context.guestResp = buf
			return []wasmer.Value{}, nil
		},
	)
	i.guestError = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes()),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			ptr := args[0].I32()
			len := args[1].I32()
			data := i.mem.Data()
			cp := make([]byte, len)
			copy(cp, data[ptr:ptr+len])
			i.context.guestErr = string(cp)
			return []wasmer.Value{}, nil
		},
	)
	i.hostCall = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			bindingPtr := args[0].I32()
			bindingLen := args[1].I32()
			namespacePtr := args[2].I32()
			namespaceLen := args[3].I32()
			operationPtr := args[4].I32()
			operationLen := args[5].I32()
			payloadPtr := args[6].I32()
			payloadLen := args[7].I32()

			if i.m.hostCallHandler == nil {
				return []wasmer.Value{wasmer.NewI32(0)}, nil
			}

			data := i.mem.Data()
			binding := string(data[bindingPtr : bindingPtr+bindingLen])
			namespace := string(data[namespacePtr : namespacePtr+namespaceLen])
			operation := string(data[operationPtr : operationPtr+operationLen])
			payload := make([]byte, payloadLen)
			copy(payload, data[payloadPtr:payloadPtr+payloadLen])

			i.context.hostResp, i.context.hostErr = i.m.hostCallHandler(i.context.ctx, binding, namespace, operation, payload)
			if i.context.hostErr != nil {
				return []wasmer.Value{wasmer.NewI32(0)}, nil
			}

			return []wasmer.Value{wasmer.NewI32(1)}, nil
		},
	)
	i.hostResponseLen = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			return []wasmer.Value{wasmer.NewI32(int32(len(i.context.hostResp)))}, nil
		},
	)
	i.hostResponse = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32), wasmer.NewValueTypes()),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			if i.context.hostResp != nil {
				ptr := args[0].I32()
				data := i.mem.Data()
				copy(data[ptr:], i.context.hostResp)
			}
			return []wasmer.Value{}, nil
		},
	)
	i.hostErrorLen = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			if i.context.hostErr == nil {
				return []wasmer.Value{wasmer.NewI32(0)}, nil
			}
			errStr := i.context.hostErr.Error()
			return []wasmer.Value{wasmer.NewI32(int32(len(errStr)))}, nil
		},
	)
	i.hostError = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32), wasmer.NewValueTypes()),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			if i.context.hostErr == nil {
				return []wasmer.Value{}, nil
			}

			ptr := args[0].I32()
			errStr := i.context.hostErr.Error()
			data := i.mem.Data()
			copy(data[ptr:], errStr)
			return []wasmer.Value{}, nil
		},
	)
	i.consoleLog = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes()),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			if i.m.logger != nil {
				data := i.mem.Data()
				ptr := args[0].I32()
				len := args[1].I32()
				msg := string(data[ptr : ptr+len])
				i.m.logger(msg)
			}
			return []wasmer.Value{}, nil
		},
	)
	return map[string]wasmer.IntoExtern{
		"__guest_request":     i.guestRequest,
		"__guest_response":    i.guestResponse,
		"__guest_error":       i.guestError,
		"__host_call":         i.hostCall,
		"__host_response_len": i.hostResponseLen,
		"__host_response":     i.hostResponse,
		"__host_error_len":    i.hostErrorLen,
		"__host_error":        i.hostError,
		"__console_log":       i.consoleLog,
	}
}

func (i *Instance) wasiRuntime() map[string]wasmer.IntoExtern {
	i.fdWrite = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			fileDescriptor := args[0].I32()
			iovsPtr := args[1].I32()
			iovsLen := args[2].I32()
			writtenPtr := args[3].I32()

			// Only writing to standard out (1) is supported
			if fileDescriptor != 1 {
				return []wasmer.Value{wasmer.NewI32(0)}, nil
			}

			if i.m.writer == nil {
				return []wasmer.Value{wasmer.NewI32(0)}, nil
			}
			data := i.mem.Data()
			iov := data[iovsPtr:]
			bytesWritten := uint32(0)

			for iovsLen > 0 {
				iovsLen--
				base := binary.LittleEndian.Uint32(iov)
				length := binary.LittleEndian.Uint32(iov[4:])
				stringBytes := data[base : base+length]
				i.m.writer(string(stringBytes))
				iov = iov[8:]
				bytesWritten += length
			}

			binary.LittleEndian.PutUint32(data[writtenPtr:], bytesWritten)

			return []wasmer.Value{wasmer.NewI32(int32(bytesWritten))}, nil
		},
	)

	i.argsSizesGet = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			argc := args[0].I32()
			argvBufSize := args[1].I32()
			data := i.mem.Data()

			binary.LittleEndian.PutUint32(data[argc:], 0)
			binary.LittleEndian.PutUint32(data[argvBufSize:], 0)

			return []wasmer.Value{wasmer.NewI32(0)}, nil
		},
	)

	i.argsGet = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			return []wasmer.Value{wasmer.NewI32(0)}, nil
		},
	)

	return map[string]wasmer.IntoExtern{
		"fd_write":       i.fdWrite,
		"args_sizes_get": i.argsSizesGet,
		"args_get":       i.argsGet,
	}
}

// MemorySize returns the memory length of the underlying instance.
func (i *Instance) MemorySize() uint32 {
	return uint32(i.mem.DataSize())
}

// Invoke calls `operation` with `payload` on the module and returns a byte slice payload.
func (i *Instance) Invoke(ctx context.Context, operation string, payload []byte) ([]byte, error) {
	// Make sure instance isn't closed to avoid panics
	if i.inst == nil {
		return nil, fmt.Errorf("error invoking guest with closed instance")
	}

	context := invokeContext{
		ctx:       ctx,
		operation: operation,
		guestReq:  payload,
	}
	i.context = &context

	successValue, err := i.guestCall(len(operation), len(payload))
	if err != nil {
		if context.guestErr != "" {
			return nil, errors.New(context.guestErr)
		}
		return nil, fmt.Errorf("error invoking guest: %w", err)
	}
	successI32, _ := successValue.(int32)
	success := successI32 == 1

	if success {
		return context.guestResp, nil
	}

	return nil, fmt.Errorf("call to %q was unsuccessful", operation)
}

// Close closes the single instance.  This should be called before calling `Close` on the Module itself.
func (i *Instance) Close() {
	// Explicitly release references on wasmer types so they can be GC'ed.
	i.inst = nil
	i.mem = nil
	i.context = nil
	i.guestRequest = nil
	i.guestResponse = nil
	i.guestError = nil
	i.hostCall = nil
	i.hostResponseLen = nil
	i.hostResponse = nil
	i.hostErrorLen = nil
	i.hostError = nil
	i.consoleLog = nil
	i.abort = nil
	i.fdWrite = nil
}

// Close closes the module.  This should be called after calling `Close` on any instances that were created.
func (m *Module) Close() {
	// Explicitly release references on wasmer types so they can be GC'ed.
	m.module = nil
	m.store = nil
	m.engine = nil
}

// Println will print the supplied message to standard error. Newline is appended to the end of the message.
func Println(message string) {
	println(message)
}

// Print will print the supplied message to standard error.
func Print(message string) {
	print(message)
}
