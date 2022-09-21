package wapc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Workiva/go-datastructures/queue"
)

type (
	// Pool is a wrapper around a ringbuffer of WASM modules
	Pool struct {
		rb        *queue.RingBuffer
		module    Module
		instances []Instance
	}

	InstanceInitialize func(instance Instance) error
)

// NewPool takes in compiled WASM module and a size and returns a pool
// containing `size` instances of that module.
func NewPool(ctx context.Context, module Module, size uint64, initializer ...InstanceInitialize) (*Pool, error) {
	var initialize InstanceInitialize
	if len(initializer) > 0 {
		initialize = initializer[0]
	}
	rb := queue.NewRingBuffer(size)
	instances := make([]Instance, size)
	for i := uint64(0); i < size; i++ {
		inst, err := module.Instantiate(ctx)
		if err != nil {
			return nil, err
		}

		if initialize != nil {
			if err = initialize(inst); err != nil {
				return nil, fmt.Errorf("could not initialize instance: %w", err)
			}
		}

		ok, err := rb.Offer(inst)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("could not add module %d to module pool of size %d", i, size)
		}

		instances[i] = inst
	}

	return &Pool{
		rb:        rb,
		module:    module,
		instances: instances,
	}, nil
}

// Get returns a module from the pool if it can be retrieved
// within the passed timeout window, if not it returns an error
func (p *Pool) Get(timeout time.Duration) (Instance, error) {
	instanceIface, err := p.rb.Poll(timeout)
	if err != nil {
		return nil, fmt.Errorf("get from pool timed out: %w", err)
	}

	inst, ok := instanceIface.(Instance)
	if !ok {
		return nil, errors.New("item retrieved from pool is not an instance")
	}

	return inst, nil
}

// Return takes a module and adds it to the pool
// This should only be called using a module
func (p *Pool) Return(inst Instance) error {
	ok, err := p.rb.Offer(inst)
	if err != nil {
		return err
	}

	if !ok {
		return errors.New("cannot return instance to full pool")
	}

	return nil
}

// Close closes down all the instances contained by the pool.
func (p *Pool) Close(ctx context.Context) {
	p.rb.Dispose()

	for _, inst := range p.instances {
		inst.Close(ctx)
	}
}
