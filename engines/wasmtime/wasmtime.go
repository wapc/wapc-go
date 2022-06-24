//go:build (((amd64 || arm64) && !windows) || (amd64 && windows)) && cgo && !wasmer

package wasmtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/bytecodealliance/wasmtime-go"

	wapc "github.com/JanFalkin/wapc-go"
)

type (
	engine struct{}

	// Module represents a compile waPC module.
	Module struct {
		logger wapc.Logger // Logger to use for waPC's __console_log
		writer wapc.Logger // Logger to use for WASI fd_write (where fd == 1 for standard out)

		hostCallHandler wapc.HostCallHandler

		engine *wasmtime.Engine
		store  *wasmtime.Store
		module *wasmtime.Module

		// closed is atomically updated to ensure Close is only invoked once.
		closed uint32
	}

	// Instance is a single instantiation of a module with its own memory.
	Instance struct {
		m         *Module
		guestCall *wasmtime.Func

		inst *wasmtime.Instance
		mem  *wasmtime.Memory

		context *invokeContext

		// waPC functions
		guestRequest    *wasmtime.Func
		guestResponse   *wasmtime.Func
		guestError      *wasmtime.Func
		hostCall        *wasmtime.Func
		hostResponseLen *wasmtime.Func
		hostResponse    *wasmtime.Func
		hostErrorLen    *wasmtime.Func
		hostError       *wasmtime.Func
		consoleLog      *wasmtime.Func

		// AssemblyScript functions
		abort *wasmtime.Func

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
	return "wasmtime"
}

// New compiles a `Module` from `code`.
func (e *engine) doNew(engine *wasmtime.Engine, code []byte, hostCallHandler wapc.HostCallHandler) (wapc.Module, error) {
	store := wasmtime.NewStore(engine)

	wasiConfig := wasmtime.NewWasiConfig()
	store.SetWasi(wasiConfig)

	module, err := wasmtime.NewModule(engine, code)
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

func (e *engine) NewWithDebug(code []byte, hostCallHandler wapc.HostCallHandler) (wapc.Module, error) {
	cfg := wasmtime.NewConfig()
	cfg.SetDebugInfo(true)
	engine := wasmtime.NewEngineWithConfig(cfg)
	return e.doNew(engine, code, hostCallHandler)
}

func (e *engine) NewWithMetering(code []byte, hostCallHandler wapc.HostCallHandler, maxInstructions uint64, pfn unsafe.Pointer) (wapc.Module, error) {
	return e.New(context.TODO(), code, hostCallHandler)
}

func (i *Instance) RemainingPoints(context.Context) uint64 {
	return 0
}

// New compiles a `Module` from `code`.
func (e *engine) New(ctx context.Context, code []byte, hostCallHandler wapc.HostCallHandler) (wapc.Module, error) {
	engine := wasmtime.NewEngine()
	return e.doNew(engine, code, hostCallHandler)
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

	linker := wasmtime.NewLinker(m.engine)
	if err := linker.DefineWasi(); err != nil {
		return nil, err
	}

	for name, fn := range instance.envRuntime() {
		if err := linker.Define("env", name, fn); err != nil {
			return nil, fmt.Errorf("Cannot define function env.%s: %w", name, err)
		}
	}

	for name, fn := range instance.wapcRuntime() {
		if err := linker.Define("wapc", name, fn); err != nil {
			return nil, fmt.Errorf("Cannot define function wapc.%s: %w", name, err)
		}
	}

	inst, err := linker.Instantiate(m.store, m.module)
	if err != nil {
		return nil, err
	}
	instance.inst = inst

	instance.mem = inst.GetExport(m.store, "memory").Memory()
	if err != nil {
		return nil, err
	}

	instance.guestCall = inst.GetFunc(m.store, "__guest_call")
	if instance.guestCall == nil {
		return nil, errors.New("could not find exported function '__guest_call'")
	}

	// Initialize the instance of it exposes a `_start` function.
	initFunctions := []string{"_start", "wapc_init"}
	for _, initFunction := range initFunctions {
		if initFn := inst.GetFunc(m.store, initFunction); initFn != nil {
			context := invokeContext{
				ctx: ctx,
			}
			instance.context = &context

			if _, err := initFn.Call(m.store); err != nil {
				return nil, fmt.Errorf("could not initialize instance: %w", err)
			}
		}
	}

	return &instance, nil
}

func (i *Instance) envRuntime() map[string]*wasmtime.Func {
	// abort is for assemblyscript
	params := []*wasmtime.ValType{
		wasmtime.NewValType(wasmtime.KindI32),
		wasmtime.NewValType(wasmtime.KindI32),
		wasmtime.NewValType(wasmtime.KindI32),
		wasmtime.NewValType(wasmtime.KindI32),
	}
	results := []*wasmtime.ValType{}

	i.abort = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(params, results),
		func(caller *wasmtime.Caller, params []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			return []wasmtime.Val{}, nil
		},
	)

	return map[string]*wasmtime.Func{
		"abort": i.abort,
	}
}

func (i *Instance) wapcRuntime() map[string]*wasmtime.Func {
	i.guestRequest = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			operationPtr := args[0].I32()
			payloadPtr := args[1].I32()
			data := i.mem.UnsafeData(i.m.store)
			copy(data[operationPtr:], i.context.operation)
			copy(data[payloadPtr:], i.context.guestReq)
			return []wasmtime.Val{}, nil
		},
	)

	i.guestResponse = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			ptr := args[0].I32()
			len := args[1].I32()
			data := i.mem.UnsafeData(i.m.store)
			buf := make([]byte, len)
			copy(buf, data[ptr:ptr+len])
			i.context.guestResp = buf
			return []wasmtime.Val{}, nil
		},
	)

	i.guestError = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			ptr := args[0].I32()
			len := args[1].I32()
			data := i.mem.UnsafeData(i.m.store)
			cp := make([]byte, len)
			copy(cp, data[ptr:ptr+len])
			i.context.guestErr = string(cp)
			return []wasmtime.Val{}, nil
		},
	)

	i.hostCall = wasmtime.NewFunc(
		i.m.store,

		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
			},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			bindingPtr := args[0].I32()
			bindingLen := args[1].I32()
			namespacePtr := args[2].I32()
			namespaceLen := args[3].I32()
			operationPtr := args[4].I32()
			operationLen := args[5].I32()
			payloadPtr := args[6].I32()
			payloadLen := args[7].I32()

			if i.m.hostCallHandler == nil {
				return []wasmtime.Val{wasmtime.ValI32(0)}, nil
			}

			data := i.mem.UnsafeData(i.m.store)
			binding := string(data[bindingPtr : bindingPtr+bindingLen])
			namespace := string(data[namespacePtr : namespacePtr+namespaceLen])
			operation := string(data[operationPtr : operationPtr+operationLen])
			payload := make([]byte, payloadLen)
			copy(payload, data[payloadPtr:payloadPtr+payloadLen])

			i.context.hostResp, i.context.hostErr = i.m.hostCallHandler(i.context.ctx, binding, namespace, operation, payload)
			if i.context.hostErr != nil {
				return []wasmtime.Val{wasmtime.ValI32(0)}, nil
			}

			return []wasmtime.Val{wasmtime.ValI32(1)}, nil
		},
	)

	i.hostResponseLen = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{},
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
			},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			return []wasmtime.Val{wasmtime.ValI32(int32(len(i.context.hostResp)))}, nil
		},
	)

	i.hostResponse = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			if i.context.hostResp != nil {
				ptr := args[0].I32()
				data := i.mem.UnsafeData(i.m.store)
				copy(data[ptr:], i.context.hostResp)
			}
			return []wasmtime.Val{}, nil
		},
	)

	i.hostErrorLen = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{},
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
			},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			if i.context.hostErr == nil {
				return []wasmtime.Val{wasmtime.ValI32(0)}, nil
			}
			errStr := i.context.hostErr.Error()
			return []wasmtime.Val{wasmtime.ValI32(int32(len(errStr)))}, nil
		},
	)

	i.hostError = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			if i.context.hostErr == nil {
				return []wasmtime.Val{}, nil
			}

			ptr := args[0].I32()
			errStr := i.context.hostErr.Error()
			data := i.mem.UnsafeData(i.m.store)
			copy(data[ptr:], errStr)
			return []wasmtime.Val{}, nil
		},
	)

	i.consoleLog = wasmtime.NewFunc(
		i.m.store,
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{},
		),
		func(c *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			if i.m.logger != nil {
				data := i.mem.UnsafeData(i.m.store)
				ptr := args[0].I32()
				len := args[1].I32()
				msg := string(data[ptr : ptr+len])
				i.m.logger(msg)
			}
			return []wasmtime.Val{}, nil
		},
	)

	return map[string]*wasmtime.Func{
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

// MemorySize returns the memory length of the underlying instance.
func (i *Instance) MemorySize(context.Context) uint32 {
	return uint32(i.mem.DataSize(i.m.store))
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

	successValue, err := i.guestCall.Call(i.m.store, len(operation), len(payload))
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

	// Explicitly release references on wasmtime types, so they can be GC'ed.
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
	return nil // wasmtime only closes via finalizer
}

// Close closes the module.  This should be called after calling `Close` on any instances that were created.
func (m *Module) Close(context.Context) error {
	if !atomic.CompareAndSwapUint32(&m.closed, 0, 1) {
		return nil
	}

	// Explicitly release references on wasmtime types, so they can be GC'ed.
	m.module = nil
	if store := m.store; store != nil {
		store.GC()
		m.store = nil
	}
	m.engine = nil
	return nil // wasmtime only closes via finalizer
}
