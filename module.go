package wapc

import (
	"context"
	"encoding/binary"
	"unsafe"

	"github.com/pkg/errors"
	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

// #include <stdlib.h>
//
// extern void __guest_request(void *context, int32_t operation_ptr, int32_t payload_ptr);
// extern void __guest_response(void *context, int32_t ptr, int32_t len);
// extern void __guest_error(void *context, int32_t ptr, int32_t len);
//
// extern int32_t __host_call(void *context, int32_t binding_ptr, int32_t binding_len, int32_t namespace_ptr, int32_t namespace_len, int32_t operation_ptr, int32_t operation_len, int32_t payload_ptr, int32_t payload_len);
// extern int32_t __host_response_len(void *context);
// extern void __host_response(void *context, int32_t ptr);
// extern int32_t __host_error_len(void *context);
// extern void __host_error(void *context, int32_t ptr);
//
// extern void __console_log(void *context, int32_t ptr, int32_t len);
// extern int32_t __fd_write(void *context, int32_t arg1, int32_t arg2, int32_t arg3, int32_t arg4);
//
// extern void abortModule(void *context, int32_t ptr1, int32_t len1, int32_t ptr2, int32_t len2);
import "C"

type (
	// Logger is the function to call from consoleLog inside a waPC module.
	Logger func(msg string)

	// HostCallHandler is a function to invoke to handle when a guest is performing a host call.
	HostCallHandler func(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error)

	// Module represents a compile waPC module.
	Module struct {
		logger          Logger // Logger to use for waPC's __console_log
		writer          Logger // Logger to use for WASI fd_write (where fd == 1 for standard out)
		module          wasm.Module
		hostCallHandler HostCallHandler
	}

	// Instance is a single instantiation of a module with its own memory.
	Instance struct {
		m         *Module
		instance  wasm.Instance
		guestCall func(...interface{}) (wasm.Value, error)
	}
)

var imports *wasm.Imports

func init() {
	imports = wasm.NewImports()
	imports.Append("abort", abortModule, C.abortModule)
	imports.Namespace("wapc")
	imports.AppendFunction("__guest_request", __guest_request, C.__guest_request)
	imports.AppendFunction("__guest_response", __guest_response, C.__guest_response)
	imports.AppendFunction("__guest_error", __guest_error, C.__guest_error)
	imports.AppendFunction("__host_call", __host_call, C.__host_call)
	imports.AppendFunction("__host_response_len", __host_response_len, C.__host_response_len)
	imports.AppendFunction("__host_response", __host_response, C.__host_response)
	imports.AppendFunction("__host_error_len", __host_error_len, C.__host_error_len)
	imports.AppendFunction("__host_error", __host_error, C.__host_error)
	imports.AppendFunction("__console_log", __console_log, C.__console_log)
	imports = imports.Namespace("wasi_unstable")
	imports.AppendFunction("fd_write", __fd_write, C.__fd_write)
	imports = imports.Namespace("wasi_snapshot_preview1")
	imports.AppendFunction("fd_write", __fd_write, C.__fd_write)
}

// NoOpHostCallHandler is an noop host call handler to use if your host does not need to support host calls.
func NoOpHostCallHandler(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
	return []byte{}, nil
}

// New compiles a `Module` from `code`.
func New(code []byte, hostCallHandler HostCallHandler) (*Module, error) {
	module, err := wasm.Compile(code)
	if err != nil {
		return nil, err
	}

	return &Module{
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
	instance, err := m.module.InstantiateWithImports(imports)
	if err != nil {
		return nil, err
	}

	// Initialize the instance of it exposes a `_start` function.
	initFunctions := []string{"_start", "wapc_init"}
	for _, initFunction := range initFunctions {
		if initFn, ok := instance.Exports[initFunction]; ok {
			context := functionContext{
				logger: m.logger,
				writer: m.writer,
				ctx:    context.Background(),
			}
			instance.SetContextData(&context)

			if _, err := initFn(); err != nil {
				return nil, errors.Wrap(err, "could not initialize instance")
			}
		}
	}

	guestCall, ok := instance.Exports["__guest_call"]
	if !ok {
		return nil, errors.New("could not find exported function '__guest_call'")
	}

	return &Instance{
		m:         m,
		instance:  instance,
		guestCall: guestCall,
	}, nil
}

// MemorySize returns the memory length of the underlying instance.
func (i *Instance) MemorySize() uint32 {
	return i.instance.Memory.Length()
}

// Invoke calls `operation` with `payload` on the module and returns a byte slice payload.
func (i *Instance) Invoke(ctx context.Context, operation string, payload []byte) ([]byte, error) {
	context := functionContext{
		logger:          i.m.logger,
		writer:          i.m.writer,
		ctx:             ctx,
		operation:       operation,
		guestReq:        payload,
		hostCallHandler: i.m.hostCallHandler,
	}
	i.instance.SetContextData(&context)

	successValue, err := i.guestCall(len(operation), len(payload))
	if err != nil {
		if context.guestErr != "" {
			return nil, errors.WithStack(errors.New(context.guestErr))
		}
		return nil, errors.Wrap(err, "error invoking guest")
	}
	success := successValue.ToI32() == 1

	if success {
		return context.guestResp, nil
	}

	return nil, errors.WithStack(errors.Errorf("call to %q was unsuccessful", operation))
}

// Close closes the single instance.  This should be called before calling `Close` on the Module itself.
func (i *Instance) Close() {
	i.instance.Close()
}

// Close closes the module.  This should be called after calling `Close` on any instances that were created.
func (m *Module) Close() {
	m.module.Close()
}

//export __guest_request
func __guest_request(context unsafe.Pointer, operationPtr int32, payloadPtr int32) {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	imp.guestRequest(instanceContext.Memory(), operationPtr, payloadPtr)
}

//export __guest_response
func __guest_response(context unsafe.Pointer, ptr int32, length int32) {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	imp.guestResponse(instanceContext.Memory(), ptr, length)
}

//export __guest_error
func __guest_error(context unsafe.Pointer, ptr int32, length int32) {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	imp.guestError(instanceContext.Memory(), ptr, length)
}

//export __host_call
func __host_call(context unsafe.Pointer, bindingPtr int32, bindingLen int32, namespacePtr int32, namespaceLen int32, operationPtr int32, operationLen int32, payloadPtr int32, payloadLen int32) int32 {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	return imp.hostCall(instanceContext.Memory(), bindingPtr, bindingLen, namespacePtr, namespaceLen, operationPtr, operationLen, payloadPtr, payloadLen)
}

//export __host_response_len
func __host_response_len(context unsafe.Pointer) int32 {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	return imp.hostResponseLen(instanceContext.Memory())
}

//export __host_response
func __host_response(context unsafe.Pointer, payloadPtr int32) {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	imp.hostResponse(instanceContext.Memory(), payloadPtr)
}

//export __host_error_len
func __host_error_len(context unsafe.Pointer) int32 {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	return imp.hostErrorLen(instanceContext.Memory())
}

//export __host_error
func __host_error(context unsafe.Pointer, payloadPtr int32) {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	imp.hostError(instanceContext.Memory(), payloadPtr)
}

//export __console_log
func __console_log(context unsafe.Pointer, str int32, length int32) {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	imp.consoleLog(instanceContext.Memory(), str, length)
}

//export __fd_write
func __fd_write(context unsafe.Pointer, fileDescriptor, iovsPtr, iovsLen, writtenPtr int32) int32 {
	instanceContext := wasm.IntoInstanceContext(context)
	imp := instanceContext.Data().(*functionContext)
	return imp.fdWrite(instanceContext.Memory(), fileDescriptor, iovsPtr, iovsLen, writtenPtr)
}

//export abortModule
func abortModule(context unsafe.Pointer, msgPtr int32, filePtr int32, line int32, col int32) {
}

type functionContext struct {
	logger    Logger
	writer    Logger
	ctx       context.Context
	operation string
	guestReq  []byte
	guestResp []byte
	guestErr  string

	hostCallHandler HostCallHandler
	hostResp        []byte
	hostErr         error
}

func (i *functionContext) guestRequest(memory *wasm.Memory, operationPtr int32, payloadPtr int32) {
	data := memory.Data()
	copy(data[operationPtr:], i.operation)
	copy(data[payloadPtr:], i.guestReq)
}

func (i *functionContext) guestResponse(memory *wasm.Memory, ptr int32, length int32) {
	data := memory.Data()
	buf := make([]byte, length)
	copy(buf, data[ptr:ptr+length])
	i.guestResp = buf
}

func (i *functionContext) guestError(memory *wasm.Memory, ptr int32, len int32) {
	data := memory.Data()
	cp := make([]byte, len)
	copy(cp, data[ptr:ptr+len])
	i.guestErr = string(cp)
}

func (i *functionContext) hostCall(memory *wasm.Memory, bindingPtr int32, bindingLen int32, namespacePtr int32, namespaceLen int32, operationPtr int32, operationLen int32, payloadPtr int32, payloadLen int32) int32 {
	if i.hostCallHandler == nil {
		return 0
	}

	data := memory.Data()
	binding := string(data[bindingPtr : bindingPtr+bindingLen])
	namespace := string(data[namespacePtr : namespacePtr+namespaceLen])
	operation := string(data[operationPtr : operationPtr+operationLen])
	payload := make([]byte, payloadLen)
	copy(payload, data[payloadPtr:payloadPtr+payloadLen])

	i.hostResp, i.hostErr = i.hostCallHandler(i.ctx, binding, namespace, operation, payload)
	if i.hostErr != nil {
		return 0
	}

	return 1
}

func (i *functionContext) hostResponseLen(memory *wasm.Memory) int32 {
	return int32(len(i.hostResp))
}

func (i *functionContext) hostResponse(memory *wasm.Memory, ptr int32) {
	if i.hostResp == nil {
		return
	}
	data := memory.Data()
	copy(data[ptr:], i.hostResp)
}

func (i *functionContext) hostErrorLen(memory *wasm.Memory) int32 {
	if i.hostErr == nil {
		return 0
	}
	errStr := i.hostErr.Error()
	return int32(len(errStr))
}

func (i *functionContext) hostError(memory *wasm.Memory, ptr int32) {
	if i.hostErr == nil {
		return
	}
	errStr := i.hostErr.Error()
	data := memory.Data()
	copy(data[ptr:], errStr)
}

func (i *functionContext) consoleLog(memory *wasm.Memory, str int32, len int32) {
	if i.logger != nil {
		data := memory.Data()
		msg := string(data[str : str+len])
		i.logger(msg)
	}
}

func (i *functionContext) fdWrite(memory *wasm.Memory, fileDescriptor, iovsPtr, iovsLen, writtenPtr int32) int32 {
	// Only writing to standard out (1) is supported
	if fileDescriptor != 1 {
		return 0
	}

	if i.writer == nil {
		return 0
	}
	data := memory.Data()
	iov := data[iovsPtr:]
	bytesWritten := uint32(0)

	for iovsLen > 0 {
		iovsLen--
		base := binary.LittleEndian.Uint32(iov)
		length := binary.LittleEndian.Uint32(iov[4:])
		stringBytes := data[base : base+length]
		i.writer(string(stringBytes))
		iov = iov[8:]
		bytesWritten += length
	}

	binary.LittleEndian.PutUint32(data[writtenPtr:], bytesWritten)

	return int32(bytesWritten)
}

func Println(message string) {
	println(message)
}

func Print(message string) {
	print(message)
}
