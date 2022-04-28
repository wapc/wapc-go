package wapc_test

import (
	"testing"

	"github.com/JanFalkin/wapc-go"
	"github.com/JanFalkin/wapc-go/engines/wazero"
)

var wazeroEngine = []wapc.Engine{wazero.Engine()}

func TestWazeroGuests(t *testing.T) {
	testGuests(t, wazeroEngine)
}

func TestWazeroModuleBadBytes(t *testing.T) {
	testModuleBadBytes(t, wazeroEngine)
}

func TestWazeroModule(t *testing.T) {
	testModule(t, wazeroEngine)
}
func TestWazeroGuestsWithPool(t *testing.T) {
	testGuestsWithPool(t, wazeroEngine)
}
