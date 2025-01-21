build:
	@echo "----------"
	@echo "Building Go wasm Guest"
	@echo "----------"
	tinygo build -o hello.wasm -scheduler=none --no-debug -target=wasip1 -buildmode=c-shared main.go
