# Compiling the Go guest example

This host runtime will support running Go modules. However, due to the evolving nature of WASM support within Go, 
it must be compiled with a TinyGo version < 0.18.0 following the example command below.

```console
$ tinygo build -o hello.wasm -target wasi main.go
```

The `hello.wasm` file included in this directory was compiled with TinyGo v0.17.0.
