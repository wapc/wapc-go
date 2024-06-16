module github.com/wapc/wapc-go/example

go 1.23.0

toolchain go1.23.4

require (
	github.com/wapc/wapc-go v0.0.0
	github.com/wapc/wapc-go/engines/wazero v0.0.0
)

require (
	github.com/Workiva/go-datastructures v1.1.5 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
)

replace (
	github.com/wapc/wapc-go => ../
	github.com/wapc/wapc-go/engines/wazero => ../engines/wazero
)
