# waPC Host for Go [![Gitter](https://badges.gitter.im/wapc/community.svg)](https://gitter.im/wapc/community)

This is the Golang implementation of the **waPC** standard for WebAssembly host runtimes. It allows any WebAssembly module to be loaded as a guest and receive requests for invocation as well as to make its own function requests of the host.

## Example

The following is a simple example of synchronous, bidirectional procedure calls between a WebAssembly host runtime and the guest module.

```go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/JanFalkin/wapc-go"
	"github.com/JanFalkin/wapc-go/engines/wazero"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: hello <name>")
		return
	}
	name := os.Args[1]
	ctx := context.Background()
	code, err := os.ReadFile("hello/hello.wasm")
	if err != nil {
		panic(err)
	}

	engine := wazero.Engine()

	module, err := engine.New(ctx, code, hostCall)
	if err != nil {
		panic(err)
	}
	module.SetLogger(wapc.Println)
	module.SetWriter(wapc.Print)
	defer module.Close(ctx)

	instance, err := module.Instantiate(ctx)
	if err != nil {
		panic(err)
	}
	defer instance.Close(ctx)

	result, err := instance.Invoke(ctx, "hello", []byte(name))
	if err != nil {
		panic(err)
	}

	fmt.Println(string(result))
}

func hostCall(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
	// Route the payload to any custom functionality accordingly.
	// You can even route to other waPC modules!!!
	switch namespace {
	case "example":
		switch operation {
		case "capitalize":
			name := string(payload)
			name = strings.Title(name)
			return []byte(name), nil
		}
	}
	return []byte("default"), nil
}
```

To see this in action, enter the following in your shell:

```
$ go run example/main.go waPC!
hello called
Hello, WaPC!
```

Alternatively you can use a `Pool` to manage a pool of instances.

```go
	pool, err := wapc.NewPool(ctx, module, 10)
	if err != nil {
		panic(err)
	}
	defer pool.Close(ctx)

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

While the above example uses wazero, wapc-go is decoupled (via [`wapc.Engine`](#engines)) and can be used with different runtimes.

## Engines

Here are the supported `wapc.Engine` implementations, in alphabetical order:

|    Name     |        Usage        | Build Tag |                                                Package                                                |
|:-----------:|:-------------------:|-----------|:-----------------------------------------------------------------------------------------------------:|
|  wasmer-go  |  `wasmer.Engine()`  | wasmer    |           [github.com/wasmerio/wasmer-go](https://pkg.go.dev/github.com/wasmerio/wasmer-go)           |
| wasmtime-go | `wasmtime.Engine()` | wasmtime  | [github.com/bytecodealliance/wasmtime-go](https://pkg.go.dev/github.com/bytecodealliance/wasmtime-go) |
|   wazero    |  `wazero.Engine()`  | N/A       |                          [github.com/tetratelabs/wazero](https://wazero.io)                           |


For example, to switch the engine to wasmer, change [example/main.go](example/main.go) like below:
```diff
--- a/example/main.go
+++ b/example/main.go
@@ -7,7 +7,7 @@ import (
        "strings"

        "github.com/JanFalkin/wapc-go"
-       "github.com/JanFalkin/wapc-go/engines/wazero"
+       "github.com/JanFalkin/wapc-go/engines/wasmer"
 )

 func main() {
@@ -22,7 +22,7 @@ func main() {
                panic(err)
        }

-       engine := wazero.Engine()
+       engine := wasmer.Engine()

        module, err := engine.New(ctx, code, hostCall)
        if err != nil {
```

Then, run with its build tag:
```bash
$ go run --tags wasmer example/main.go waPC!
hello called
Hello, WaPC!
```

### Differences with [wapc-rs](https://github.com/wapc/wapc-rs) (Rust)

Besides engine choices, there differences between this library and the Rust implementation:
* Separate compilation (`New`) and instantiation (`Instantiate`) steps. This is to incur the cost of compilation once in a multi-instance scenario.
* `Pool` for creating a pool of instances for a given Module.
