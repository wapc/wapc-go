package wapc_test

import (
	"context"
	"io/ioutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wapc/wapc-go"
)

func TestPool(t *testing.T) {
	ctx := context.Background()
	code, err := ioutil.ReadFile("testdata/go/hello.wasm")
	require.NoError(t, err)

	hostCall := func(ctx context.Context, binding, namespace, operation string, payload []byte) ([]byte, error) {
		return []byte("test"), nil
	}

	module, err := wapc.New(code, hostCall)
	require.NoError(t, err)
	defer module.Close()

	pool, err := wapc.NewPool(module, 10)
	require.NoError(t, err)
	defer pool.Close()

	for i := 0; i < 100; i++ {
		instance, err := pool.Get(10 * time.Millisecond)
		require.NoError(t, err)

		result, err := instance.Invoke(ctx, "hello", []byte("waPC"))
		require.NoError(t, err)

		assert.Equal(t, "Hello, waPC", string(result))
		err = pool.Return(instance)
		require.NoError(t, err)
	}
}
