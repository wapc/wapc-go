module github.com/wapc/wapc-go/engines/wazero

go 1.23.0

toolchain go1.23.4

require (
	github.com/tetratelabs/wazero v1.9.0
	github.com/wapc/wapc-go v0.0.0
)

require (
	github.com/Workiva/go-datastructures v1.1.5 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/wapc/wapc-go => ../..
