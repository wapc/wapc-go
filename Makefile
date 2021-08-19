# Makefile to build an execute tests

tests: 
	mkdir -p .coverage
	go test -v -covermode=count -coverprofile=.coverage/coverage.out ./...
	go tool cover -html=.coverage/coverage.out -o .coverage/coverage.html

build-data: build-as build-go build-rust

build-as:
	$(MAKE) -C testdata/as build

build-go:
	$(MAKE) -C testdata/go build

build-rust:
	$(MAKE) -C testdata/rust build
