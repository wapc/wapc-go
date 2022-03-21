package wazero

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/wasm"

	"github.com/wapc/wapc-go"
)

// functionStart is the name of the nullary function a module must export if it is a WASI Command.
// See https://github.com/WebAssembly/WASI/blob/snapshot-01/design/application-abi.md#current-unstable-abi
const functionStart = "_start"

// functionInit is the name of the nullary function that initializes wapc.
// Note: This must be run after functionStart
const functionInit = "wapc_init"

// functionGuestCall is a callback required to be exported. Below is its signature in WebAssembly 1.0 (MVP) Text Format:
//	(func $__guest_call (param $operation_len i32) (param $payload_len i32) (result (;errno;) i32))
const functionGuestCall = "__guest_call"

type (
	engine struct{}

	// Module represents a compile waPC module.
	Module struct {
		logger wapc.Logger // Logger to use for waPC's __console_log
		writer wapc.Logger // Logger to use for WASI fd_write (where fd == 1 for standard out)

		hostCallHandler wapc.HostCallHandler

		engine *wazero.Engine
		store  wasm.Store
		module *wazero.ModuleConfig

		instanceCounter uint64
	}

	Instance struct {
		name      string
		m         wasm.ModuleExports
		guestCall wasm.Function
		closed    bool
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
	store := wazero.NewStoreWithConfig(&wazero.StoreConfig{
		Context: context.Background(),
		Engine:  getEngine(),
	})

	module := &wazero.ModuleConfig{Source: code}
	if err := module.Validate(); err != nil {
		return nil, err
	}

	m := &Module{
		store:           store,
		module:          module,
		hostCallHandler: hostCallHandler,
	}

	if _, err := wazero.InstantiateHostModule(store, wazero.WASISnapshotPreview1()); err != nil {
		return nil, err
	}

	if _, err := wazero.InstantiateHostModule(store, &wazero.HostModuleConfig{
		Name: "env",
		Functions: map[string]interface{}{
			"abort": m.env_abort,
		},
	}); err != nil {
		return nil, err
	}

	if _, err := wazero.InstantiateHostModule(store, &wazero.HostModuleConfig{
		Name: "wapc",
		Functions: map[string]interface{}{
			"__guest_request":     m.wapc_guest_request,
			"__guest_response":    m.wapc_guest_response,
			"__guest_error":       m.wapc_guest_error,
			"__host_call":         m.wapc_host_call,
			"__host_response_len": m.wapc_host_response_len,
			"__host_response":     m.wapc_host_response,
			"__host_error_len":    m.wapc_host_error_len,
			"__host_error":        m.wapc_host_error,
			"__console_log":       m.wapc_console_log,
		},
	}); err != nil {
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

// env_abort is the AssemblyScript abort handler
func (m *Module) env_abort(ctx wasm.ModuleContext, messageOffset, fileOffset, line, col uint32) {
	// TODO signal somewhere or call Close()? We don't need to indirect through WASI proc_raise I think
}

func (m *Module) wapc_guest_request(ctx wasm.ModuleContext, operationPtr, payloadPtr uint32) {
	ic := fromInvokeContext(ctx.Context())
	mem := ctx.Memory()
	mem.Write(operationPtr, []byte(ic.operation))
	mem.Write(payloadPtr, ic.guestReq)
}

func (m *Module) wapc_guest_response(ctx wasm.ModuleContext, ptr, len uint32) {
	ic := fromInvokeContext(ctx.Context())
	ic.guestResp = requireRead(ctx.Memory(), "guestResp", ptr, len)
}

func (m *Module) wapc_guest_error(ctx wasm.ModuleContext, ptr, len uint32) {
	ic := fromInvokeContext(ctx.Context())
	ic.guestErr = requireReadString(ctx.Memory(), "guestErr", ptr, len)
}

func (m *Module) wapc_host_call(ctx wasm.ModuleContext,
	bindingPtr, bindingLen, namespacePtr, namespaceLen,
	operationPtr, operationLen, payloadPtr, payloadLen uint32) int32 {
	if m.hostCallHandler == nil {
		return 0
	}

	goCtx := ctx.Context()
	ic := fromInvokeContext(goCtx)
	mem := ctx.Memory()
	binding := requireReadString(mem, "binding", bindingPtr, bindingLen)
	namespace := requireReadString(mem, "namespace", namespacePtr, namespaceLen)
	operation := requireReadString(mem, "operation", operationPtr, operationLen)
	payload := requireRead(mem, "payload", payloadPtr, payloadLen)

	ic.hostResp, ic.hostErr = m.hostCallHandler(goCtx, binding, namespace, operation, payload)
	if ic.hostErr != nil {
		return 0
	}

	return 1
}

func (m *Module) wapc_host_response_len(ctx wasm.ModuleContext) int32 {
	ic := fromInvokeContext(ctx.Context())
	return int32(len(ic.hostResp))
}

func (m *Module) wapc_host_response(ctx wasm.ModuleContext, ptr uint32) {
	ic := fromInvokeContext(ctx.Context())
	if ic.hostResp != nil {
		ctx.Memory().Write(ptr, ic.hostResp)
	}
}

func (m *Module) wapc_host_error_len(ctx wasm.ModuleContext) int32 {
	ic := fromInvokeContext(ctx.Context())
	if ic.hostErr == nil {
		return 0
	}
	errStr := ic.hostErr.Error()
	return int32(len(errStr))
}

func (m *Module) wapc_host_error(ctx wasm.ModuleContext, ptr uint32) {
	ic := fromInvokeContext(ctx.Context())
	if ic.hostErr == nil {
		return
	}

	errStr := ic.hostErr.Error()
	ctx.Memory().Write(ptr, []byte(errStr))
}

func (m *Module) wapc_console_log(ctx wasm.ModuleContext, ptr, len uint32) {
	if m.logger != nil {
		msg := requireReadString(ctx.Memory(), "msg", ptr, len)
		m.logger(msg)
	}
}

// Instantiate creates a single instance of the module with its own memory.
func (m *Module) Instantiate() (wapc.Instance, error) {
	moduleName := fmt.Sprintf("%d", atomic.AddUint64(&m.instanceCounter, 1))

	// Don't use wazero.StartWASICommand as the input module may not define _start
	exports, err := wazero.InstantiateModule(m.store, m.module.WithName(moduleName))
	if err != nil {
		return nil, err
	}

	instance := Instance{
		name:      moduleName,
		m:         exports,
		guestCall: exports.Function(functionGuestCall),
	}

	if instance.guestCall == nil {
		return nil, fmt.Errorf("module %s didn't export function %s", moduleName, functionGuestCall)
	}

	// Initialize the instance of it exposes a `_start` function.
	initFunctions := []string{functionStart, functionInit}
	ctx := context.Background()
	for _, name := range initFunctions {
		if fn := exports.Function(name); fn == nil {
			continue // init functions are optional
		} else if _, err = fn.Call(ctx); err != nil {
			return nil, fmt.Errorf("module[%s] function[%s] failed: %w", moduleName, name, err)
		}
	}

	return &instance, nil
}

// MemorySize returns the memory length of the underlying instance.
func (i *Instance) MemorySize() uint32 {
	return i.m.Memory("memory").Size() // "memory" is the required name of the memory export in WASI
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

	results, err := i.guestCall.Call(ctx, uint64(len(operation)), uint64(len(payload)))
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
	// TODO: i.module.Close() https://github.com/tetratelabs/wazero/issues/293
}

// Close closes the module.  This should be called after calling `Close` on any instances that were created.
func (m *Module) Close() {
	m.module = nil
	m.store = nil
	m.engine = nil
}

// requireReadString is a convenience function that casts requireRead
func requireReadString(mem wasm.Memory, fieldName string, offset, byteCount uint32) string {
	return string(requireRead(mem, fieldName, offset, byteCount))
}

// requireRead is like wasm.Memory except that it panics if the offset and byteCount are out of range.
func requireRead(mem wasm.Memory, fieldName string, offset, byteCount uint32) []byte {
	buf, ok := mem.Read(offset, byteCount)
	if !ok {
		panic(fmt.Errorf("out of range reading %s", fieldName))
	}
	return buf
}
