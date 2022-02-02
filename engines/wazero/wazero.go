package wazero

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/tetratelabs/wazero/wasi"
	"github.com/tetratelabs/wazero/wasm"
	"github.com/tetratelabs/wazero/wasm/binary"

	"github.com/wapc/wapc-go"
)

type (
	engine struct{}

	// Module represents a compile waPC module.
	Module struct {
		logger wapc.Logger // Logger to use for waPC's __console_log
		writer wapc.Logger // Logger to use for WASI fd_write (where fd == 1 for standard out)

		hostCallHandler wapc.HostCallHandler

		engine wasm.Engine
		store  *wasm.Store
		module *wasm.Module

		instanceCounter uint64
	}

	Instance struct {
		name   string
		m      *Module
		closed bool
	}

	invokeContext struct {
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
	return "wazero"
}

// New compiles a `Module` from `code`.
func (e *engine) New(code []byte, hostCallHandler wapc.HostCallHandler) (wapc.Module, error) {
	module, err := binary.DecodeModule(code)
	if err != nil {
		return nil, err
	}

	engine := getEngine()
	store := wasm.NewStore(engine)

	if err = wasi.NewEnvironment().Register(store); err != nil {
		return nil, err
	}

	m := &Module{
		engine:          engine,
		store:           store,
		module:          module,
		hostCallHandler: hostCallHandler,
	}

	if err = store.AddHostFunction("env", "abort", reflect.ValueOf(m.env_abort)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__guest_request", reflect.ValueOf(m.wapc_guest_request)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__guest_response", reflect.ValueOf(m.wapc_guest_response)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__guest_error", reflect.ValueOf(m.wapc_guest_error)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__host_call", reflect.ValueOf(m.wapc_host_call)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__host_response_len", reflect.ValueOf(m.wapc_host_response_len)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__host_response", reflect.ValueOf(m.wapc_host_response)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__host_error_len", reflect.ValueOf(m.wapc_host_error_len)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__host_error", reflect.ValueOf(m.wapc_host_error)); err != nil {
		return nil, err
	}
	if err = store.AddHostFunction("wapc", "__console_log", reflect.ValueOf(m.wapc_console_log)); err != nil {
		return nil, err
	}

	return m, nil
}

// SetLogger sets the waPC logger for __console_log calls.
func (m *Module) SetLogger(logger wapc.Logger) {
	m.logger = logger
}

// SetWriter sets the writer for WASI fd_write calls to standard out.
func (m *Module) SetWriter(writer wapc.Logger) {
	m.writer = writer
}

func (m *Module) env_abort(ctx *wasm.HostFunctionCallContext, arg1 int32, arg2 int32, arg3 int32, arg4 int32) {
}

func (m *Module) wapc_guest_request(ctx *wasm.HostFunctionCallContext, operationPtr, payloadPtr int32) {
	ic := fromInvokeContext(ctx.Context())
	data := ctx.Memory.Buffer
	copy(data[operationPtr:], ic.operation)
	copy(data[payloadPtr:], ic.guestReq)
}

func (m *Module) wapc_guest_response(ctx *wasm.HostFunctionCallContext, ptr, len int32) {
	ic := fromInvokeContext(ctx.Context())
	data := ctx.Memory.Buffer
	buf := make([]byte, len)
	copy(buf, data[ptr:ptr+len])
	ic.guestResp = buf
}

func (m *Module) wapc_guest_error(ctx *wasm.HostFunctionCallContext, ptr, len int32) {
	ic := fromInvokeContext(ctx.Context())
	data := ctx.Memory.Buffer
	cp := make([]byte, len)
	copy(cp, data[ptr:ptr+len])
	ic.guestErr = string(cp)
}

func (m *Module) wapc_host_call(ctx *wasm.HostFunctionCallContext,
	bindingPtr, bindingLen, namespacePtr, namespaceLen,
	operationPtr, operationLen, payloadPtr, payloadLen int32) int32 {
	if m.hostCallHandler == nil {
		return 0
	}

	goCtx := ctx.Context()
	ic := fromInvokeContext(goCtx)
	data := ctx.Memory.Buffer
	binding := string(data[bindingPtr : bindingPtr+bindingLen])
	namespace := string(data[namespacePtr : namespacePtr+namespaceLen])
	operation := string(data[operationPtr : operationPtr+operationLen])
	payload := make([]byte, payloadLen)
	copy(payload, data[payloadPtr:payloadPtr+payloadLen])

	ic.hostResp, ic.hostErr = m.hostCallHandler(goCtx, binding, namespace, operation, payload)
	if ic.hostErr != nil {
		return 0
	}

	return 1
}

func (m *Module) wapc_host_response_len(ctx *wasm.HostFunctionCallContext) int32 {
	ic := fromInvokeContext(ctx.Context())
	return int32(len(ic.hostResp))
}

func (m *Module) wapc_host_response(ctx *wasm.HostFunctionCallContext, ptr int32) {
	ic := fromInvokeContext(ctx.Context())
	if ic.hostResp != nil {
		data := ctx.Memory.Buffer
		copy(data[ptr:], ic.hostResp)
	}
}

func (m *Module) wapc_host_error_len(ctx *wasm.HostFunctionCallContext) int32 {
	ic := fromInvokeContext(ctx.Context())
	if ic.hostErr == nil {
		return 0
	}
	errStr := ic.hostErr.Error()
	return int32(len(errStr))
}

func (m *Module) wapc_host_error(ctx *wasm.HostFunctionCallContext, ptr int32) {
	ic := fromInvokeContext(ctx.Context())
	if ic.hostErr == nil {
		return
	}

	data := ctx.Memory.Buffer
	errStr := ic.hostErr.Error()
	copy(data[ptr:], errStr)
}

func (m *Module) wapc_console_log(ctx *wasm.HostFunctionCallContext, ptr, len int32) {
	if m.logger != nil {
		data := ctx.Memory.Buffer
		msg := string(data[ptr : ptr+len])
		m.logger(msg)
	}
}

// Instantiate creates a single instance of the module with its own memory.
func (m *Module) Instantiate() (wapc.Instance, error) {
	moduleName := fmt.Sprintf("%d", atomic.AddUint64(&m.instanceCounter, 1))
	if err := m.store.Instantiate(m.module, moduleName); err != nil {
		return nil, err
	}

	instance := Instance{
		name: moduleName,
		m:    m,
	}

	// Initialize the instance of it exposes a `_start` function.
	initFunctions := []string{"_start", "wapc_init"}
	ctx := context.Background()
	for _, initFunction := range initFunctions {
		m.store.CallFunction(ctx, moduleName, initFunction) //nolint: errcheck
	}

	return &instance, nil
}

// MemorySize returns the memory length of the underlying instance.
func (i *Instance) MemorySize() uint32 {
	return uint32(len(i.m.store.ModuleInstances[i.name].Memory.Buffer))
}

type invokeContextKey struct{}

func newInvokeContext(ctx context.Context, ic *invokeContext) context.Context {
	return context.WithValue(ctx, invokeContextKey{}, ic)
}

func fromInvokeContext(ctx context.Context) *invokeContext {
	ic, _ := ctx.Value(invokeContextKey{}).(*invokeContext)
	if ic == nil {
		ic = &invokeContext{}
	}
	return ic
}

// Invoke calls `operation` with `payload` on the module and returns a byte slice payload.
func (i *Instance) Invoke(ctx context.Context, operation string, payload []byte) ([]byte, error) {
	// Make sure instance isn't closed
	if i.closed {
		return nil, fmt.Errorf("error invoking guest with closed instance")
	}

	ic := invokeContext{
		operation: operation,
		guestReq:  payload,
	}
	ctx = newInvokeContext(ctx, &ic)

	results, _, err := i.m.store.CallFunction(ctx, i.name, "__guest_call", uint64(len(operation)), uint64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("error invoking guest: %w", err)
	}
	if ic.guestErr != "" {
		return nil, errors.New(ic.guestErr)
	}

	result := results[0]
	success := result == 1

	if success {
		return ic.guestResp, nil
	}

	return nil, fmt.Errorf("call to %q was unsuccessful", operation)
}

// Close closes the single instance.  This should be called before calling `Close` on the Module itself.
func (i *Instance) Close() {
	i.closed = true
}

// Close closes the module.  This should be called after calling `Close` on any instances that were created.
func (m *Module) Close() {
	m.module = nil
	m.store = nil
	m.engine = nil
}
