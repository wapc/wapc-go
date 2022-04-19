package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/wapc/wapc-go"
	"github.com/wapc/wapc-go/engines/wasmtime"
)

type Settings struct {
	ModulePath   string
	WaPCFunction string
	Message      string
}

func cli() Settings {
	var modulePath, wapcFunction string

	flag.StringVar(&modulePath, "m", "", "Path to the Wasm module to be loaded")
	flag.StringVar(&wapcFunction, "f", "echo", "Name of the waPC function to invoke")

	flag.Parse()
	if modulePath == "" {
		os.Stderr.WriteString("Must provide path to the Wasm module to load")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if flag.NArg() == 0 {
		os.Stderr.WriteString("Must provide payload message for waPC function")
		flag.PrintDefaults()
		os.Exit(1)
	}
	msg := flag.Arg(0)

	return Settings{
		ModulePath:   modulePath,
		Message:      msg,
		WaPCFunction: wapcFunction,
	}
}

func main() {
	settings := cli()

	ctx := context.Background()
	code, err := os.ReadFile(settings.ModulePath)
	if err != nil {
		panic(err)
	}

	engine := wasmtime.Engine()

	module, err := engine.New(ctx, code, hostCall)
	if err != nil {
		panic(err)
	}
	module.SetLogger(wapc.Println)
	module.SetWriter(wapc.Print)
	defer module.Close()

	instance, err := module.Instantiate(ctx)
	if err != nil {
		panic(err)
	}
	defer instance.Close()

	result, err := instance.Invoke(ctx, settings.WaPCFunction, []byte(settings.Message))
	if err != nil {
		panic(err)
	}

	fmt.Println(string(result))
}

func hostCall(_ context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
	log.Println("host callback")
	log.Printf("binding: %s\n", binding)
	log.Printf("namespace: %s\n", namespace)
	log.Printf("operation: %s\n", operation)
	log.Printf("payload: %s\n", string(payload))
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
	case "testing":
		switch operation {
		case "echo":
			return []byte(fmt.Sprintf("echo: %s", payload)), nil // echo
		}
	}
	return []byte("default"), nil
}
