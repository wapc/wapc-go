build:
	@echo "----------"
	@echo "Building Go wasm Guest"
	@echo "----------"
	${TINYGOPATH}tinygo build -o hello.wasm -scheduler=none --no-debug -target=wasi main.go
