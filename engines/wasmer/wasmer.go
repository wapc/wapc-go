//go:build (amd64 || arm64) && !windows && cgo && !wasmtime

package wasmer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wasmerio/wasmer-go/wasmer"

	wapc "github.com/JanFalkin/wapc-go"
)

type (
	engine struct{}

	// Module represents a compile waPC module.
	Module struct {
		logger wapc.Logger // Logger to use for waPC's __console_log
		writer wapc.Logger // Logger to use for WASI fd_write (where fd == 1 for standard out)

		hostCallHandler wapc.HostCallHandler

		engine *wasmer.Engine
		store  *wasmer.Store
		module *wasmer.Module

		// closed is atomically updated to ensure Close is only invoked once.
		closed uint32
	}

	// Instance is a single instantiation of a module with its own memory.
	Instance struct {
		m         *Module
		guestCall func(...interface{}) (interface{}, error)

		inst *wasmer.Instance
		mem  *wasmer.Memory

		context *invokeContext

		// waPC functions
		guestRequest    *wasmer.Function
		guestResponse   *wasmer.Function
		guestError      *wasmer.Function
		hostCall        *wasmer.Function
		hostResponseLen *wasmer.Function
		hostResponse    *wasmer.Function
		hostErrorLen    *wasmer.Function
		hostError       *wasmer.Function
		consoleLog      *wasmer.Function

		// AssemblyScript functions
		abort *wasmer.Function

		// WASI functions
		fdWrite          *wasmer.Function
		fdClose          *wasmer.Function
		fdFdstatGet      *wasmer.Function
		fdPrestatGet     *wasmer.Function
		fdPrestatDirName *wasmer.Function
		fdRead           *wasmer.Function
		fdSeek           *wasmer.Function
		pathOpen         *wasmer.Function
		procExit         *wasmer.Function
		argsSizesGet     *wasmer.Function
		argsGet          *wasmer.Function
		clockTimeGet     *wasmer.Function
		environSizesGet  *wasmer.Function
		environGet       *wasmer.Function

		// closed is atomically updated to ensure Close is only invoked once.
		closed uint32
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

// Ensure the engine conforms to the waPC interface.
var _ = (wapc.Module)((*Module)(nil))
var _ = (wapc.Instance)((*Instance)(nil))

var engineInstance = engine{}

func Engine() wapc.Engine {
	return &engineInstance
}

func (e *engine) Name() string {
	return "wasmer"
}

func (e *engine) doNew(code []byte, hostCallHandler wapc.HostCallHandler, engine *wasmer.Engine, store *wasmer.Store) (wapc.Module, error) {
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

// New compiles a `Module` from `code`.
func (e *engine) NewWithMetering(code []byte, hostCallHandler wapc.HostCallHandler, maxInstructions uint64, pfn unsafe.Pointer) (wapc.Module, error) {
	config := wasmer.NewConfig().PushMeteringMiddlewarePtr(maxInstructions, pfn)
	engine := wasmer.NewEngineWithConfig(config)
	store := wasmer.NewStore(engine)
	return e.doNew(code, hostCallHandler, engine, store)
}

// New compiles a `Module` from `code`.
func (e *engine) New(ctx context.Context, code []byte, hostCallHandler wapc.HostCallHandler) (wapc.Module, error) {
	engine := wasmer.NewEngine()
	store := wasmer.NewStore(engine)
	return e.doNew(code, hostCallHandler, engine, store)
}

func (e *engine) NewWithDebug(code []byte, hostCallHandler wapc.HostCallHandler) (wapc.Module, error) {
	return e.New(nil, code, hostCallHandler)
}

// SetLogger sets the waPC logger for __console_log calls.
func (m *Module) SetLogger(logger wapc.Logger) {
	m.logger = logger
}

// SetWriter sets the writer for WASI fd_write calls to standard out.
func (m *Module) SetWriter(writer wapc.Logger) {
	m.writer = writer
}

// Instantiate creates a single instance of the module with its own memory.
func (m *Module) Instantiate(ctx context.Context) (wapc.Instance, error) {
	if closed := atomic.LoadUint32(&m.closed); closed != 0 {
		return nil, errors.New("cannot Instantiate when a module is closed")
	}
	// Note: There's still a race below, even if the above check is still useful.

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
				ctx: ctx,
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

	i.fdClose = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(8)}, nil
		},
	)

	i.fdPrestatGet = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(8)}, nil
		},
	)

	i.fdPrestatDirName = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(8)}, nil
		},
	)

	i.fdRead = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(8)}, nil
		},
	)

	i.fdSeek = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I64, wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(8)}, nil
		},
	)

	i.pathOpen = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32, wasmer.I32,
			wasmer.I64, wasmer.I64, wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(28)}, nil
		},
	)

	i.procExit = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32), wasmer.NewValueTypes()),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(0)}, nil
		},
	)

	i.fdFdstatGet = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(8)}, nil
		},
	)

	i.argsSizesGet = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
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
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(0)}, nil
		},
	)

	i.environSizesGet = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(0)}, nil
		},
	)

	i.environGet = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			// Not implemented.
			return []wasmer.Value{wasmer.NewI32(0)}, nil
		},
	)

	i.clockTimeGet = wasmer.NewFunction(
		i.m.store,
		wasmer.NewFunctionType(wasmer.NewValueTypes(wasmer.I32, wasmer.I64, wasmer.I32), wasmer.NewValueTypes(wasmer.I32)),
		func(args []wasmer.Value) ([]wasmer.Value, error) {
			//(ctx *wasm.HostFunctionCallContext, id uint32, precision uint64, timestampPtr uint32) (err Errno) {
			data := i.mem.Data()
			timestampPtr := args[2].I32()
			nanos := uint64(time.Now().UnixNano())
			binary.LittleEndian.PutUint64(data[timestampPtr:], nanos)
			return []wasmer.Value{wasmer.NewI32(0)}, nil
		},
	)

	return map[string]wasmer.IntoExtern{
		"fd_write":            i.fdWrite,
		"fd_close":            i.fdClose,
		"fd_fdstat_get":       i.fdFdstatGet,
		"fd_prestat_get":      i.fdPrestatGet,
		"fd_prestat_dir_name": i.fdPrestatDirName,
		"fd_read":             i.fdRead,
		"fd_seek":             i.fdSeek,
		"path_open":           i.pathOpen,
		"proc_exit":           i.procExit,
		"args_sizes_get":      i.argsSizesGet,
		"args_get":            i.argsGet,
		"environ_sizes_get":   i.environSizesGet,
		"environ_get":         i.environGet,
		"clock_time_get":      i.clockTimeGet,
	}
}

// MemorySize returns the memory length of the underlying instance.
func (i *Instance) MemorySize(context.Context) uint32 {
	return uint32(i.mem.DataSize())
}

// Invoke calls `operation` with `payload` on the module and returns a byte slice payload.
func (i *Instance) Invoke(ctx context.Context, operation string, payload []byte) ([]byte, error) {
	if closed := atomic.LoadUint32(&i.closed); closed != 0 {
		return nil, fmt.Errorf("error invoking guest with closed instance")
	}
	// Note: There's still a race below, even if the above check is still useful.

	context := invokeContext{
		ctx:       ctx,
		operation: operation,
		guestReq:  payload,
	}
	i.context = &context

	successValue, err := i.guestCall(len(operation), len(payload))
	if err != nil {
		return nil, fmt.Errorf("error invoking guest: %w", err)
	}
	if context.guestErr != "" {
		return nil, errors.New(context.guestErr)
	}

	successI32, _ := successValue.(int32)
	success := successI32 == 1

	if success {
		return context.guestResp, nil
	}

	return nil, fmt.Errorf("call to %q was unsuccessful", operation)
}

// Close closes the single instance.  This should be called before calling `Close` on the Module itself.
func (i *Instance) Close(context.Context) error {
	if !atomic.CompareAndSwapUint32(&i.closed, 0, 1) {
		return nil
	}

	// Explicitly release references on wasmer types, so they can be GC'ed.
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
	if inst := i.inst; inst != nil {
		inst.Close()
	}
	return nil
}

// Close closes the module.  This should be called after calling `Close` on any instances that were created.
func (m *Module) Close(context.Context) error {
	if !atomic.CompareAndSwapUint32(&m.closed, 0, 1) {
		return nil
	}

	// Explicitly release references on wasmer types so they can be GC'ed.
	if mod := m.module; mod != nil {
		mod.Close()
		m.module = nil
	}
	if store := m.store; store != nil {
		store.Close()
		m.store = nil
	}
	m.engine = nil
	return nil
}

func (i *Instance) RemainingPoints(context.Context) uint64 {
	return i.inst.GetRemainingPoints()
}
