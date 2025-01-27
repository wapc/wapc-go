module github.com/wapc/wapc-go/engines/wasmtime

go 1.23.0

toolchain go1.23.4

require (
	github.com/bytecodealliance/wasmtime-go v1.0.0
	github.com/wapc/wapc-go v0.0.0
)

require github.com/Workiva/go-datastructures v1.1.5 // indirect

replace github.com/wapc/wapc-go => ../..
