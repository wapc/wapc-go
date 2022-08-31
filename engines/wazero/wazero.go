package wazero

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/assemblyscript"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/wapc/wapc-go"
)

// functionStart is the name of the nullary function a module exports if it is a WASI Command Module.
//
// See https://github.com/WebAssembly/WASI/blob/snapshot-01/design/application-abi.md#current-unstable-abi
const functionStart = "_start"

// functionInit is the name of the nullary function that initializes waPC.
const functionInit = "wapc_init"

// functionGuestCall is a callback required to be exported. Below is its signature in WebAssembly 1.0 (MVP) Text Format:
//
//	(func $__guest_call (param $operation_len i32) (param $payload_len i32) (result (;errno;) i32))
const functionGuestCall = "__guest_call"

type (
	engine struct{ newRuntime NewRuntime }

	// Module represents a compiled waPC module.
	Module struct {
		// wapcHostCallHandler is the value of wapcHost.callHandler
		wapcHostCallHandler wapc.HostCallHandler

		runtime  wazero.Runtime
		compiled wazero.CompiledModule

		instanceCounter uint64

		config wazero.ModuleConfig

		// closed is atomically updated to ensure Close is only invoked once.
		closed uint32
	}

	Instance struct {
		name      string
		m         api.Module
		guestCall api.Function

		// closed is atomically updated to ensure Close is only invoked once.
		closed uint32
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

var engineInstance = engine{newRuntime: DefaultRuntime}

// Engine returns a new wapc.Engine which uses the DefaultRuntime.
func Engine() wapc.Engine {
	return &engineInstance
}

// NewRuntime returns a new wazero runtime which is called when the New method
// on wapc.Engine is called. The result is closed upon wapc.Module Close.
type NewRuntime func(context.Context) (wazero.Runtime, error)

// EngineWithRuntime allows you to customize or return an alternative to
// DefaultRuntime,
func EngineWithRuntime(newRuntime NewRuntime) wapc.Engine {
	return &engine{newRuntime: newRuntime}
}

func (e *engine) Name() string {
	return "wazero"
}

func (e *engine) NewWithMetering(code []byte, hostCallHandler wapc.HostCallHandler, maxInstructions uint64, pfn unsafe.Pointer) (wapc.Module, error) {
	ctx := context.Background()
	return e.New(ctx, hostCallHandler, code, nil)
}

func (e *engine) NewWithDebug(code []byte, hostCallHandler wapc.HostCallHandler) (wapc.Module, error) {
	ctx := context.Background()
	return e.New(ctx, hostCallHandler, code, nil)
}

// DefaultRuntime implements NewRuntime by returning a wazero runtime with WASI
// and AssemblyScript host functions instantiated.
func DefaultRuntime(ctx context.Context) (wazero.Runtime, error) {
	rc := wazero.NewRuntimeConfig().WithWasmCore2()
	r := wazero.NewRuntimeWithConfig(ctx, rc)

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, err
	}

	// This disables the abort message as no other engines write it.
	envBuilder := r.NewModuleBuilder("env")
	assemblyscript.NewFunctionExporter().WithAbortMessageDisabled().ExportFunctions(envBuilder)
	if _, err := envBuilder.Instantiate(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, err
	}
	return r, nil
}

// New implements the same method as documented on wapc.Engine.
func (e *engine) New(ctx context.Context, host wapc.HostCallHandler, guest []byte, config *wapc.ModuleConfig) (mod wapc.Module, err error) {
	r, err := e.newRuntime(ctx)
	if err != nil {
		return nil, err
	}

	m := &Module{runtime: r, wapcHostCallHandler: host}

	m.config = wazero.NewModuleConfig().
		WithStartFunctions(functionStart, functionInit) // Call any WASI or waPC start functions on instantiate.

	if config.Stdout != nil {
		m.config = m.config.WithStdout(config.Stdout)
	}
	if config.Stderr != nil {
		m.config = m.config.WithStderr(config.Stderr)
	}
	mod = m

	if _, err = instantiateWapcHost(ctx, r, m.wapcHostCallHandler, config.Logger); err != nil {
		_ = r.Close(ctx)
		return
	}

	if m.compiled, err = r.CompileModule(ctx, guest, wazero.NewCompileConfig()); err != nil {
		_ = r.Close(ctx)
		return
	}
	return
}

// wapcHost implements all required waPC host function exports.
//
// See https://wapc.io/docs/spec/#required-host-exports
type wapcHost struct {
	// callHandler implements hostCall, which returns false (0) when nil.
	callHandler wapc.HostCallHandler

	// logger is used to implement consoleLog.
	logger wapc.Logger
}

// instantiateWapcHost instantiates a wapcHost and returns it and its corresponding module, or an error.
//   - r: used to instantiate the waPC host module
//   - callHandler: used to implement hostCall
//   - logger: used to implement consoleLog
func instantiateWapcHost(ctx context.Context, r wazero.Runtime, callHandler wapc.HostCallHandler, logger wapc.Logger) (api.Module, error) {
	h := &wapcHost{callHandler: callHandler, logger: logger}
	// Export host functions (in the order defined in https://wapc.io/docs/spec/#required-host-exports)
	return r.NewModuleBuilder("wapc").
		ExportFunction("__host_call", h.hostCall,
			"__host_call", "bind_ptr", "bind_len", "ns_ptr", "ns_len", "cmd_ptr", "cmd_len", "payload_ptr", "payload_len").
		ExportFunction("__console_log", h.consoleLog,
			"__console_log", "ptr", "len").
		ExportFunction("__guest_request", h.guestRequest,
			"__guest_request", "op_ptr", "ptr").
		ExportFunction("__host_response", h.hostResponse,
			"__host_response", "ptr").
		ExportFunction("__host_response_len", h.hostResponseLen,
			"__host_response_len").
		ExportFunction("__guest_response", h.guestResponse,
			"__guest_response", "ptr", "len").
		ExportFunction("__guest_error", h.guestError,
			"__guest_error", "ptr", "len").
		ExportFunction("__host_error", h.hostError,
			"__host_error", "ptr").
		ExportFunction("__host_error_len", h.hostErrorLen,
			"__host_error_len").
		Instantiate(ctx, r)
}

// hostCall is the WebAssembly function export "__host_call", which initiates a host using the callHandler using
// parameters read from linear memory (wasm.Memory).
func (w *wapcHost) hostCall(ctx context.Context, m api.Module, bindPtr, bindLen, nsPtr, nsLen, cmdPtr, cmdLen, payloadPtr, payloadLen uint32) int32 {
	ic := fromInvokeContext(ctx)
	if ic == nil || w.callHandler == nil {
		return 0 // false: there's neither an invocation context, nor a callHandler
	}

	mem := m.Memory()
	binding := requireReadString(ctx, mem, "binding", bindPtr, bindLen)
	namespace := requireReadString(ctx, mem, "namespace", nsPtr, nsLen)
	operation := requireReadString(ctx, mem, "operation", cmdPtr, cmdLen)
	payload := requireRead(ctx, mem, "payload", payloadPtr, payloadLen)

	if ic.hostResp, ic.hostErr = w.callHandler(ctx, binding, namespace, operation, payload); ic.hostErr != nil {
		return 0 // false: there was an error (assumed to be logged already?)
	}

	return 1 // true
}

// consoleLog is the WebAssembly function export "__console_log", which logs the message stored by the guest at the
// given offset (ptr) and length (len) in linear memory (wasm.Memory).
func (w *wapcHost) consoleLog(ctx context.Context, m api.Module, ptr, len uint32) {
	if log := w.logger; log != nil {
		msg := requireReadString(ctx, m.Memory(), "msg", ptr, len)
		w.logger(msg)
	}
}

// guestRequest is the WebAssembly function export "__guest_request", which writes the invokeContext.operation and
// invokeContext.guestReq to the given offsets (opPtr, ptr) in linear memory (wasm.Memory).
func (w *wapcHost) guestRequest(ctx context.Context, m api.Module, opPtr, ptr uint32) {
	ic := fromInvokeContext(ctx)
	if ic == nil {
		return // no invoke context
	}

	mem := m.Memory()
	if operation := ic.operation; operation != "" {
		mem.Write(ctx, opPtr, []byte(operation))
	}
	if guestReq := ic.guestReq; guestReq != nil {
		mem.Write(ctx, ptr, guestReq)
	}
}

// hostResponse is the WebAssembly function export "__host_response", which writes the invokeContext.hostResp to the
// given offset (ptr) in linear memory (wasm.Memory).
func (w *wapcHost) hostResponse(ctx context.Context, m api.Module, ptr uint32) {
	if ic := fromInvokeContext(ctx); ic == nil {
		return // no invoke context
	} else if hostResp := ic.hostResp; hostResp != nil {
		m.Memory().Write(ctx, ptr, hostResp)
	}
}

// hostResponse is the WebAssembly function export "__host_response_len", which returns the length of the current host
// response from invokeContext.hostResp.
func (w *wapcHost) hostResponseLen(ctx context.Context) uint32 {
	if ic := fromInvokeContext(ctx); ic == nil {
		return 0 // no invoke context
	} else if hostResp := ic.hostResp; hostResp != nil {
		return uint32(len(hostResp))
	} else {
		return 0 // no host response
	}
}

// guestResponse is the WebAssembly function export "__guest_response", which reads invokeContext.guestResp from the
// given offset (ptr) and length (len) in linear memory (wasm.Memory).
func (w *wapcHost) guestResponse(ctx context.Context, m api.Module, ptr, len uint32) {
	if ic := fromInvokeContext(ctx); ic == nil {
		return // no invoke context
	} else {
		ic.guestResp = requireRead(ctx, m.Memory(), "guestResp", ptr, len)
	}
}

// guestError is the WebAssembly function export "__guest_error", which reads invokeContext.guestErr from the given
// offset (ptr) and length (len) in linear memory (wasm.Memory).
func (w *wapcHost) guestError(ctx context.Context, m api.Module, ptr, len uint32) {
	if ic := fromInvokeContext(ctx); ic == nil {
		return // no invoke context
	} else {
		ic.guestErr = requireReadString(ctx, m.Memory(), "guestErr", ptr, len)
	}
}

// hostError is the WebAssembly function export "__host_error", which writes the invokeContext.hostErr to the given
// offset (ptr) in linear memory (wasm.Memory).
func (w *wapcHost) hostError(ctx context.Context, m api.Module, ptr uint32) {
	if ic := fromInvokeContext(ctx); ic == nil {
		return // no invoke context
	} else if hostErr := ic.hostErr; hostErr != nil {
		m.Memory().Write(ctx, ptr, []byte(hostErr.Error()))
	}
}

// hostError is the WebAssembly function export "__host_error_len", which returns the length of the current host error
// from invokeContext.hostErr.
func (w *wapcHost) hostErrorLen(ctx context.Context) uint32 {
	if ic := fromInvokeContext(ctx); ic == nil {
		return 0 // no invoke context
	} else if hostErr := ic.hostErr; hostErr != nil {
		return uint32(len(hostErr.Error()))
	} else {
		return 0 // no host error
	}
}

// Instantiate implements the same method as documented on wapc.Module.
func (m *Module) Instantiate(ctx context.Context) (wapc.Instance, error) {
	if closed := atomic.LoadUint32(&m.closed); closed != 0 {
		return nil, errors.New("cannot Instantiate when a module is closed")
	}
	// Note: There's still a race below, even if the above check is still useful.

	moduleName := fmt.Sprintf("%d", atomic.AddUint64(&m.instanceCounter, 1))

	module, err := m.runtime.InstantiateModule(ctx, m.compiled, m.config.WithName(moduleName))
	if err != nil {
		return nil, err
	}

	instance := Instance{name: moduleName, m: module}

	if instance.guestCall = module.ExportedFunction(functionGuestCall); instance.guestCall == nil {
		_ = module.Close(ctx)
		return nil, fmt.Errorf("module %s didn't export function %s", moduleName, functionGuestCall)
	}

	return &instance, nil
}

func (i *Instance) RemainingPoints(context.Context) uint64 {
	return 0
}

// MemorySize implements the same method as documented on wapc.Instance.
func (i *Instance) MemorySize(ctx context.Context) uint32 {
	return i.m.Memory().Size(ctx)
}

type invokeContextKey struct{}

func newInvokeContext(ctx context.Context, ic *invokeContext) context.Context {
	return context.WithValue(ctx, invokeContextKey{}, ic)
}

// fromInvokeContext returns invokeContext value or nil if there was none.
//
// Note: This is never nil if called by Instance.Invoke
// TODO: It may be better to panic on nil or error as if this is nil, it is a bug in waPC, as no other path calls this.
func fromInvokeContext(ctx context.Context) *invokeContext {
	ic, _ := ctx.Value(invokeContextKey{}).(*invokeContext)
	return ic
}

// Invoke implements the same method as documented on wapc.Instance.
func (i *Instance) Invoke(ctx context.Context, operation string, payload []byte) ([]byte, error) {
	if closed := atomic.LoadUint32(&i.closed); closed != 0 {
		return nil, fmt.Errorf("error invoking guest with closed instance")
	}
	// Note: There's still a race below, even if the above check is still useful.

	ic := invokeContext{operation: operation, guestReq: payload}
	ctx = newInvokeContext(ctx, &ic)

	results, err := i.guestCall.Call(ctx, uint64(len(operation)), uint64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("error invoking guest: %w", err)
	}
	if ic.guestErr != "" { // guestErr is not nil if the guest called "__guest_error".
		return nil, errors.New(ic.guestErr)
	}

	result := results[0]
	success := result == 1

	if success { // guestResp is not nil if the guest called "__guest_response".
		return ic.guestResp, nil
	}

	return nil, fmt.Errorf("call to %q was unsuccessful", operation)
}

// Close implements the same method as documented on wapc.Instance.
func (i *Instance) Close(ctx context.Context) error {
	if !atomic.CompareAndSwapUint32(&i.closed, 0, 1) {
		return nil
	}
	return i.m.Close(ctx)
}

// Close implements the same method as documented on wapc.Module.
func (m *Module) Close(ctx context.Context) (err error) {
	if !atomic.CompareAndSwapUint32(&m.closed, 0, 1) {
		return
	}
	err = m.runtime.Close(ctx) // closes everything
	m.runtime = nil
	return
}

// requireReadString is a convenience function that casts requireRead
func requireReadString(ctx context.Context, mem api.Memory, fieldName string, offset, byteCount uint32) string {
	return string(requireRead(ctx, mem, fieldName, offset, byteCount))
}

// requireRead is like api.Memory except that it panics if the offset and byteCount are out of range.
func requireRead(ctx context.Context, mem api.Memory, fieldName string, offset, byteCount uint32) []byte {
	buf, ok := mem.Read(ctx, offset, byteCount)
	if !ok {
		panic(fmt.Errorf("out of memory reading %s", fieldName))
	}
	return buf
}
