package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wapc/wapc-go"
	"github.com/wapc/wapc-go/engines/wazero"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: hello <name>")
		return
	}
	name := os.Args[1]
	ctx := context.Background()
	guest, err := os.ReadFile("hello/hello.wasm")
	if err != nil {
		panic(err)
	}

	engine := wazero.Engine()

	module, err := engine.New(ctx, host, guest, &wapc.ModuleConfig{
		Logger: wapc.PrintlnLogger,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		panic(err)
	}
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

func host(_ context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
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
