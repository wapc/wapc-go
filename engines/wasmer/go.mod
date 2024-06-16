module github.com/wapc/wapc-go/engines/wasmer

go 1.23.0

toolchain go1.23.4

require (
	github.com/wapc/wapc-go v0.0.0
	github.com/wasmerio/wasmer-go v1.0.4
)

require github.com/Workiva/go-datastructures v1.0.53 // indirect

replace github.com/wapc/wapc-go => ../..
