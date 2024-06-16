build:
	@echo "----------"
	@echo "Building Go wasm Guest"
	@echo "----------"
	docker run -v `pwd`/:/build -w /build tinygo/tinygo:0.25.0 tinygo build -o hello.wasm -scheduler=none --no-debug -target=wasi main.go
