//go:build (amd64 || arm64) && !windows

package wazero

import (
	"github.com/tetratelabs/wazero"
)

func getEngine() *wazero.Engine {
	return wazero.NewEngineJIT()
}
