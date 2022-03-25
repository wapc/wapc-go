package wazero

import (
	"context"
	"errors"
	"fmt"
	"github.com/tetratelabs/wazero/wasi"
	"io"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/wasm"

	"github.com/wapc/wapc-go"
)

// functionInit is the name of the nullary function that initializes waPC.
const functionInit = "wapc_init"

// functionGuestCall is a callback required to be exported. Below is its signature in WebAssembly 1.0 (MVP) Text Format:
//	(func $__guest_call (param $operation_len i32) (param $payload_len i32) (result (;errno;) i32))
const functionGuestCall = "__guest_call"

type (
	runtime struct{}

	// Module represents a compiled waPC module.
	Module struct {
		// wasiStdout is used for the WASI function "fd_write", when fd==1 (STDOUT).
		//
		// Note: wapc.Logger is adapted to io.Writer with stdout.
		// Note: Until #19, we assume this is supposed to be updated at runtime, so use this field as a pointer.
		// See 	// See https://github.com/WebAssembly/WASI/blob/snapshot-01/phases/snapshot/docs.md#fd_write
		wasiStdout wapc.Logger

		// wapcHostConsoleLogger is used by wapcHost.consoleLog
		//
		// Note: Until #19, we assume this is supposed to be updated at runtime, so use this field as a pointer.
		wapcHostConsoleLogger wapc.Logger

		// wapcHostCallHandler is the value of wapcHost.callHandler
		wapcHostCallHandler wapc.HostCallHandler

		runtime wazero.Runtime
		module  *wazero.Module

		instanceCounter uint64

		wasi, assemblyScript, wapc wasm.Module
		sysConfig                  *wazero.SysConfig

		closed bool
	}

	Instance struct {
		name      string
		m         wasm.Module
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

// Ensure the runtime conforms to the waPC interface.
var _ = (wapc.Module)((*Module)(nil))
var _ = (wapc.Instance)((*Instance)(nil))

var runtimeInstance = runtime{}

func Engine() wapc.Engine {
	return &runtimeInstance
}

func (e *runtime) Name() string {
	return "wazero"
}

type stdout struct {
	// m acts as a field pointer to Module.wasiStdout until #19.
	m *Module
}

// Write implements io.Writer by invoking the Module.writer or discarding if nil.
func (s *stdout) Write(p []byte) (int, error) {
	w := s.m.wasiStdout
	if w == nil {
		return io.Discard.Write(p)
	}
	w(string(p))
	return len(p), nil
}

// New compiles a `Module` from `code`.
func (e *runtime) New(code []byte, hostCallHandler wapc.HostCallHandler) (mod wapc.Module, err error) {
	r := wazero.NewRuntime()
	m := &Module{runtime: r, wapcHostCallHandler: hostCallHandler}
	// redirect Stdout to the logger
	m.sysConfig = wazero.NewSysConfig().WithStdout(&stdout{m})
	mod = m

	if m.wasi, err = r.InstantiateModule(wazero.WASISnapshotPreview1()); err != nil {
		mod.Close()
		return
	}

	if m.assemblyScript, err = instantiateAssemblyScript(r); err != nil {
		mod.Close()
		return
	}

	if m.wapc, err = instantiateWapcHost(r, m.wapcHostCallHandler, m); err != nil {
		mod.Close()
		return
	}

	if m.module, err = r.CompileModule(code); err != nil {
		mod.Close()
		return
	}
	return
}

// SetLogger implements the same method as documented on wapc.Module.
func (m *Module) SetLogger(logger wapc.Logger) {
	m.wapcHostConsoleLogger = logger
}

// SetWriter implements the same method as documented on wapc.Module.
func (m *Module) SetWriter(writer wapc.Logger) {
	m.wasiStdout = writer
}

// assemblyScript includes "Special imports" only used In AssemblyScript when a user didn't add `import "wasi"` to their
// entry file.
//
// See https://www.assemblyscript.org/concepts.html#special-imports
// See https://www.assemblyscript.org/concepts.html#targeting-wasi
// See https://www.assemblyscript.org/compiler.html#compiler-options
// See https://github.com/AssemblyScript/assemblyscript/issues/1562
type assemblyScript struct{}

// instantiateAssemblyScript instantiates a assemblyScript and returns it and its corresponding module, or an error.
func instantiateAssemblyScript(r wazero.Runtime) (wasm.Module, error) {
	a := &assemblyScript{}
	// Only define the legacy "env" "abort" import as it is the only import supported by other engines.
	return r.NewModuleBuilder("env").ExportFunction("abort", a.envAbort).Instantiate()
}

// envAbort is called on unrecoverable errors. This is typically present in Wasm compiled from AssemblyScript, if
// assertions are enabled or errors are thrown.
//
// The implementation only performs the `proc_exit(255)` part of the default implementation, as the logging is both
// complicated (because lengths aren't provided in the signature), and should go to STDERR, which isn't defined yet in
// waPC. Moreover, all other engines stub this function (no-op, not even exit!).
//
// Here's the import in a user's module that ends up using this, in WebAssembly 1.0 (MVP) Text Format:
//	(import "env" "abort" (func $~lib/builtins/abort (param i32 i32 i32 i32)))
//
// See https://github.com/AssemblyScript/assemblyscript/blob/fa14b3b03bd4607efa52aaff3132bea0c03a7989/std/assembly/wasi/index.ts#L18
func (a *assemblyScript) envAbort(m wasm.Module, messageOffset, fileNameOffset, line, col uint32) {
	// emulate WASI proc_exit(255)
	_ = m.Close()
	panic(wasi.ExitCode(255))
}

// wapcHost implements all required waPC host function exports.
//
// See https://wapc.io/docs/spec/#required-host-exports
type wapcHost struct {
	// callHandler implements hostCall, which returns false (0) when nil.
	callHandler wapc.HostCallHandler

	// m acts as a field pointer to Module.wapcHostConsoleLogger until #19.
	m *Module
}

// instantiateWapcHost instantiates a wapcHost and returns it and its corresponding module, or an error.
// * r: used to instantiate the waPC host module
// * callHandler: used to implement hostCall
// * m: field pointer to the logger used by consoleLog
func instantiateWapcHost(r wazero.Runtime, callHandler wapc.HostCallHandler, m *Module) (wasm.Module, error) {
	h := &wapcHost{callHandler: callHandler, m: m}
	// Export host functions (in the order defined in https://wapc.io/docs/spec/#required-host-exports)
	return r.NewModuleBuilder("wapc").
		ExportFunction("__host_call", h.hostCall).
		ExportFunction("__console_log", h.consoleLog).
		ExportFunction("__guest_request", h.guestRequest).
		ExportFunction("__host_response", h.hostResponse).
		ExportFunction("__host_response_len", h.hostResponseLen).
		ExportFunction("__guest_response", h.guestResponse).
		ExportFunction("__guest_error", h.guestError).
		ExportFunction("__host_error", h.hostError).
		ExportFunction("__host_error_len", h.hostErrorLen).
		Instantiate()
}

// hostCall is the WebAssembly function export "__host_call", which initiates a host using the callHandler using
// parameters read from linear memory (wasm.Memory).
func (w *wapcHost) hostCall(m wasm.Module, bindPtr, bindLen, nsPtr, nsLen, cmdPtr, cmdLen, payloadPtr, payloadLen uint32) int32 {
	ic := fromInvokeContext(m.Context())
	if ic == nil || w.callHandler == nil {
		return 0 // false: there's neither an invoke context, nor a callHandler
	}

	mem := m.Memory()
	binding := requireReadString(mem, "binding", bindPtr, bindLen)
	namespace := requireReadString(mem, "namespace", nsPtr, nsLen)
	operation := requireReadString(mem, "operation", cmdPtr, cmdLen)
	payload := requireRead(mem, "payload", payloadPtr, payloadLen)

	if ic.hostResp, ic.hostErr = w.callHandler(m.Context(), binding, namespace, operation, payload); ic.hostErr != nil {
		return 0 // false: there was an error (assumed to be logged already?)
	}

	return 1 // true
}

// consoleLog is the WebAssembly function export "__console_log", which logs the message stored by the guest at the
// given offset (ptr) and length (len) in linear memory (wasm.Memory).
func (w *wapcHost) consoleLog(m wasm.Module, ptr, len uint32) {
	if log := w.m.wapcHostConsoleLogger; log != nil {
		msg := requireReadString(m.Memory(), "msg", ptr, len)
		log(msg)
	}
}

// guestRequest is the WebAssembly function export "__guest_request", which writes the invokeContext.operation and
// invokeContext.guestReq to the given offsets (opPtr, ptr) in linear memory (wasm.Memory).
func (w *wapcHost) guestRequest(m wasm.Module, opPtr, ptr uint32) {
	ic := fromInvokeContext(m.Context())
	if ic == nil {
		return // no invoke context
	}

	mem := m.Memory()
	if operation := ic.operation; operation != "" {
		mem.Write(opPtr, []byte(operation))
	}
	if guestReq := ic.guestReq; guestReq != nil {
		mem.Write(ptr, guestReq)
	}
}

// hostResponse is the WebAssembly function export "__host_response", which writes the invokeContext.hostResp to the
// given offset (ptr) in linear memory (wasm.Memory).
func (w *wapcHost) hostResponse(m wasm.Module, ptr uint32) {
	if ic := fromInvokeContext(m.Context()); ic == nil {
		return // no invoke context
	} else if hostResp := ic.hostResp; hostResp != nil {
		m.Memory().Write(ptr, hostResp)
	}
}

// hostResponse is the WebAssembly function export "__host_response_len", which returns the length of the current host
// response from invokeContext.hostResp.
func (w *wapcHost) hostResponseLen(m wasm.Module) uint32 {
	if ic := fromInvokeContext(m.Context()); ic == nil {
		return 0 // no invoke context
	} else if hostResp := ic.hostResp; hostResp != nil {
		return uint32(len(hostResp))
	} else {
		return 0 // no host response
	}
}

// guestResponse is the WebAssembly function export "__guest_response", which reads invokeContext.guestResp from the
// given offset (ptr) and length (len) in linear memory (wasm.Memory).
func (w *wapcHost) guestResponse(m wasm.Module, ptr, len uint32) {
	if ic := fromInvokeContext(m.Context()); ic == nil {
		return // no invoke context
	} else {
		ic.guestResp = requireRead(m.Memory(), "guestResp", ptr, len)
	}
}

// guestError is the WebAssembly function export "__guest_error", which reads invokeContext.guestErr from the given
// offset (ptr) and length (len) in linear memory (wasm.Memory).
func (w *wapcHost) guestError(m wasm.Module, ptr, len uint32) {
	if ic := fromInvokeContext(m.Context()); ic == nil {
		return // no invoke context
	} else {
		ic.guestErr = requireReadString(m.Memory(), "guestErr", ptr, len)
	}
}

// hostError is the WebAssembly function export "__host_error", which writes the invokeContext.hostErr to the given
// offset (ptr) in linear memory (wasm.Memory).
func (w *wapcHost) hostError(m wasm.Module, ptr uint32) {
	if ic := fromInvokeContext(m.Context()); ic == nil {
		return // no invoke context
	} else if hostErr := ic.hostErr; hostErr != nil {
		m.Memory().Write(ptr, []byte(hostErr.Error()))
	}
}

// hostError is the WebAssembly function export "__host_error_len", which returns the length of the current host error
// from invokeContext.hostErr.
func (w *wapcHost) hostErrorLen(m wasm.Module) uint32 {
	if ic := fromInvokeContext(m.Context()); ic == nil {
		return 0 // no invoke context
	} else if hostErr := ic.hostErr; hostErr != nil {
		return uint32(len(hostErr.Error()))
	} else {
		return 0 // no host error
	}
}

// Instantiate implements the same method as documented on wapc.Module.
func (m *Module) Instantiate() (wapc.Instance, error) {
	if m.closed {
		return nil, errors.New("cannot Instantiate when a module is closed")
	}

	moduleName := fmt.Sprintf("%d", atomic.AddUint64(&m.instanceCounter, 1))

	module, err := wazero.StartWASICommandWithConfig(m.runtime, m.module.WithName(moduleName), m.sysConfig)
	if err != nil {
		return nil, err
	}

	instance := Instance{name: moduleName, m: module}

	if instance.guestCall = module.ExportedFunction(functionGuestCall); instance.guestCall == nil {
		_ = module.Close()
		return nil, fmt.Errorf("module %s didn't export function %s", moduleName, functionGuestCall)
	}

	if init := module.ExportedFunction(functionInit); init == nil {
		// functionInit is optional
	} else if _, err = init.Call(module); err != nil {
		_ = module.Close()
		return nil, fmt.Errorf("module[%s] function[%s] failed: %w", moduleName, functionInit, err)
	}

	return &instance, nil
}

// MemorySize implements the same method as documented on wapc.Instance.
func (i *Instance) MemorySize() uint32 {
	return i.m.Memory().Size()
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
	// Make sure instance isn't closed
	if i.closed {
		return nil, fmt.Errorf("error invoking guest with closed instance")
	}

	ic := invokeContext{operation: operation, guestReq: payload}
	ctx = newInvokeContext(ctx, &ic)

	results, err := i.guestCall.Call(i.m.WithContext(ctx), uint64(len(operation)), uint64(len(payload)))
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
func (i *Instance) Close() {
	i.closed = true
	_ = i.m.Close()
}

// Close implements the same method as documented on wapc.Module.
func (m *Module) Close() {
	m.closed = true

	// TODO m.runtime.Close() https://github.com/tetratelabs/wazero/issues/382
	if wapc := m.wapc; wapc != nil {
		_ = wapc.Close()
		m.wapc = nil
	}

	if env := m.assemblyScript; env != nil {
		_ = env.Close()
		m.assemblyScript = nil
	}

	if wasi := m.wasi; wasi != nil {
		_ = wasi.Close()
		m.wasi = nil
	}

	m.module = nil
	m.runtime = nil
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
