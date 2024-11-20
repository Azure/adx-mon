package crd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Store interface {
	Receive(ctx context.Context, list client.ObjectList) error
}

type Options struct {
	CtrlCli       client.Client
	List          client.ObjectList
	Store         Store
	PollFrequency time.Duration
}

type CRD struct {
	opts   Options
	ctx    context.Context
	cancel context.CancelFunc
}

func New(opts Options) *CRD {
	return &CRD{
		opts: opts,
	}
}

func (c *CRD) Open(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	if c.opts.CtrlCli == nil {
		return errors.New("no kube client provided")
	}
	if c.opts.List == nil {
		return errors.New("no list provided")
	}
	if c.opts.Store == nil {
		return errors.New("no store provided")
	}

	list, err := c.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list objects: %w", err)
	}
	if err := c.opts.Store.Receive(ctx, list); err != nil {
		return fmt.Errorf("failed to store objects: %w", err)
	}

	if c.opts.PollFrequency == 0 {
		c.opts.PollFrequency = time.Minute
	}
	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(c.opts.PollFrequency):
				list, err := c.List(ctx)
				if err != nil {
					logger.Errorf("Failed to list objects: %s", err.Error())
					continue
				}
				if err := c.opts.Store.Receive(ctx, list); err != nil {
					logger.Errorf("Failed to store objects: %s", err.Error())
					continue
				}

			}
		}
	}()

	return nil
}

func (c *CRD) Close() error {
	c.cancel()
	return nil
}
func (c *CRD) List(ctx context.Context) (client.ObjectList, error) {
	list := c.opts.List.DeepCopyObject().(client.ObjectList)
	if err := c.opts.CtrlCli.List(ctx, list); err != nil {
		if errors.Is(err, &meta.NoKindMatchError{}) {
			return list, nil
		}
		return nil, fmt.Errorf("failed to list CRDs: %w", err)
	}

	return list, nil
}
