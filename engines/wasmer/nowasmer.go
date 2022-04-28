//go:build !wasmer
// +build !wasmer

package wasmer

type engine struct{}

var engineInstance = engine{}
