build:
	@echo "----------"
	@echo "Building Rust wasm Guest"
	@echo "----------"
	cargo build --target wasm32-unknown-unknown --release
	cp target/wasm32-unknown-unknown/release/rust.wasm ./hello.wasm
