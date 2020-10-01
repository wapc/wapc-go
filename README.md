# waPC Host for Go

This is the Golang implementation of the **waPC** standard for WebAssembly host runtimes. It allows any WebAssembly module to be loaded as a guest and receive requests for invocation as well as to make its own function requests of the host.

## Example

The following is a simple example of synchronous, bi-directional procedure calls between a WebAssembly host runtime and the guest module.

```go
package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/wapc/wapc-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: hello <name>")
		return
	}
	name := os.Args[1]
	ctx := context.Background()
	code, err := ioutil.ReadFile("testdata/hello.wasm")
	if err != nil {
		panic(err)
	}

	module, err := wapc.New(code, hostCall)
	if err != nil {
		panic(err)
	}
	module.SetLogger(wapc.Println) // Send __console_log calls to stardard out
	module.SetWriter(wapc.Print) // Send WASI fd_write calls to stardard out
	defer module.Close()

	instance, err := module.Instantiate()
	if err != nil {
		panic(err)
	}
	defer instance.Close()

	result, err := instance.Invoke(ctx, "hello", []byte(name))
	if err != nil {
		panic(err)
	}

	fmt.Println(string(result))
}

func consoleLog(msg string) {
	fmt.Println(msg)
}

func hostCall(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
	// Route the payload to any custom functionality accordingly.
	// You can even route to other waPC modules!!!
	switch namespace {
	case "foo":
		switch operation {
		case "echo":
			return payload, nil // echo
		}
	}
	return []byte("default"), nil
}
```

To see this in action, enter the following in your shell:

```
$ go run example/main.go waPC!
logging something
Hello, waPC!
```

Alternatively you can use a `Pool` to manage a pool of instances.

```go
	pool, err := wapc.NewPool(module, 10)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	for i := 0; i < 100; i++ {
		instance, err := pool.Get(10 * time.Millisecond)
		if err != nil {
			panic(err)
		}

		result, err := instance.Invoke(ctx, "hello", []byte("waPC"))
		if err != nil {
			panic(err)
		}

		fmt.Println(string(result))

		err = pool.Return(instance)
		if err != nil {
			panic(err)
		}
	}
```

There are currently some differences in this library compared to the Rust implementation:

* Uses [Wasmer](https://github.com/wasmerio/wasmer) and its [Go wrapper](https://github.com/wasmerio/go-ext-wasm) for hosting WebAssembly.  We are looking into the new [Wasmtime](https://github.com/bytecodealliance/wasmtime) [Go wrapper](https://github.com/bytecodealliance/wasmtime-go).
* No support WASI... yet.
* Separate compilation (`New`) and instantiation (`Instantiate`) steps.  This is to incur the cost of compilation once in a multi-instance scenario.
* `Pool` for creating a pool of instances for a given Module.