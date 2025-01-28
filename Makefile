# Makefile to build and execute tests

.PHONY: tests
tests: 
	@echo "Executing Go tests"
	mkdir -p .coverage
	go test -v -covermode=count -coverprofile=.coverage/coverage.out \
		./engines/wasmer/... \
		./engines/wasmtime/... \
		./engines/wazero/... \
		./...
	go tool cover -html=.coverage/coverage.out -o .coverage/coverage.html

.PHONY: build-wasm
build-wasm: build-as build-example build-go build-rust

.PHONY: build-example
build-example:
	$(MAKE) -C hello build

.PHONY: build-as
build-as:
	$(MAKE) -C testdata/as build

.PHONY: build-go
build-go:
	$(MAKE) -C testdata/go build

.PHONY: build-rust
build-rust:
	$(MAKE) -C testdata/rust build
