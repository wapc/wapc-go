package wapc_test

import (
	"context"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wapc/wapc-go"
)

func TestModule(t *testing.T) {
	ctx := context.Background()
	code, err := ioutil.ReadFile("testdata/assemblyscript/hello.wasm")
	require.NoError(t, err)

	consoleLogInvoked := false
	hostCallInvoked := false

	consoleLog := func(msg string) {
		assert.Equal(t, "logging something", msg)
		consoleLogInvoked = true
	}

	hostCall := func(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
		assert.Equal(t, "myBinding", binding)
		assert.Equal(t, "sample", namespace)
		assert.Equal(t, "hello", operation)
		assert.Equal(t, "Simon", string(payload))
		hostCallInvoked = true
		return []byte("test"), nil
	}

	module, err := wapc.New(code, hostCall)
	module.SetLogger(consoleLog)
	require.NoError(t, err)
	defer module.Close()

	instance, err := module.Instantiate()
	require.NoError(t, err)
	defer instance.Close()

	result, err := instance.Invoke(ctx, "hello", []byte("waPC"))
	require.NoError(t, err)

	assert.Equal(t, "Hello, waPC", string(result))
	assert.True(t, consoleLogInvoked)
	assert.True(t, hostCallInvoked)

	result, err = instance.Invoke(ctx, "error", []byte("waPC"))
	require.Error(t, err)

	msg := err.Error()
	index := strings.IndexByte(msg, ';')
	if index != -1 {
		msg = msg[:index]
	}
	assert.Equal(t, "error occurred", msg)
}
