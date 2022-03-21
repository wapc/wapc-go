package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/wapc/wapc-go"
	"github.com/wapc/wapc-go/engines/wasmer"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: hello <name>")
		return
	}
	name := os.Args[1]
	ctx := context.Background()
	code, err := ioutil.ReadFile("hello/hello.wasm")
	if err != nil {
		panic(err)
	}

	engine := wasmer.Engine()

	module, err := engine.New(code, hostCall)
	if err != nil {
		panic(err)
	}
	module.SetLogger(wapc.Println)
	module.SetWriter(wapc.Print)
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
