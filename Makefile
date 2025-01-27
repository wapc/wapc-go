# Makefile to build and execute tests

tests: 
	@echo "Executing Go tests"
	mkdir -p .coverage
	go test -v -covermode=count -coverprofile=.coverage/coverage.out \
		./engines/wasmer/... \
		./engines/wasmtime/... \
		./engines/wazero/... \
		./...
	go tool cover -html=.coverage/coverage.out -o .coverage/coverage.html

build-wasm: build-as build-example build-go build-rust

build-example:
	$(MAKE) -C hello build

build-as:
	$(MAKE) -C testdata/as build

build-go:
	$(MAKE) -C testdata/go build

build-rust:
	$(MAKE) -C testdata/rust build
