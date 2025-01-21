package wazero_test

import (
	"testing"

	"github.com/wapc/wapc-go/engines/tests"
	"github.com/wapc/wapc-go/engines/wazero"
)

func TestGuest(t *testing.T) {
	tests.TestGuest(t, wazero.Engine())
}

func TestModuleBadBytes(t *testing.T) {
	tests.TestModuleBadBytes(t, wazero.Engine())
}

func TestModule(t *testing.T) {
	tests.TestModule(t, wazero.Engine())
}

func TestGuestsWithPool(t *testing.T) {
	tests.TestGuestsWithPool(t, wazero.Engine())
}
