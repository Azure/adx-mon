package scheduler

import (
	"context"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
)

type Elector interface {
	IsLeader() bool
}

type Periodic struct {
	cancelFn context.CancelFunc
	closing  chan struct{}
	elector  Elector
}

func NewScheduler(elector Elector) *Periodic {
	return &Periodic{
		elector: elector,
	}
}

func (s *Periodic) Open(ctx context.Context) error {
	ctx, cancelFn := context.WithCancel(ctx)
	s.cancelFn = cancelFn
	s.closing = make(chan struct{})
	return nil
}

func (s *Periodic) Close() error {
	s.cancelFn()
	close(s.closing)
	return nil
}

func (s *Periodic) ScheduleEvery(interval time.Duration, name string, fn func(ctx context.Context) error) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()

		for {
			select {
			case <-s.closing:
				return
			case <-t.C:
				if s.elector != nil && !s.elector.IsLeader() {
					continue
				}

				if err := fn(context.Background()); err != nil {
					logger.Errorf("Failed to run scheduled task %s: %s", name, err)
				}
			}
		}
	}()
}
