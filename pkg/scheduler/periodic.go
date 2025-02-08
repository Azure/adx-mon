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

// ScheduleEvery schedules a function to run at a specified interval.
// It continuously executes the provided function until the scheduler is closed.
// Scheduler implements Elector interface. This allows the scheduler to run only on the leader node.
//
// Parameters:
// - interval: The duration between each execution of the function.
// - name: A string representing the name of the scheduled task, used for logging purposes.
// - fn: The function to be executed at each interval. It receives a context parameter.
//
// The function uses a ticker to trigger the execution of the provided function at the specified interval.
// If the scheduler is closed, the function stops running.
//
// Example usage:
//
//	scheduler := NewScheduler(elector)
//	scheduler.Open(ctx)
//	scheduler.ScheduleEvery(time.Minute, "example-task", func(ctx context.Context) error {
//	    // Task implementation
//	    return nil
//	})
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

type Runner interface {
	Run(ctx context.Context) error
	Name() string
}

// RunForever runs the given runners every specified interval.
// It continuously executes the `Run` method of each provided Runner until the context is done.
// The runners are ran sequentially in a single thread.
//
// Parameters:
// - ctx: The context to control the lifecycle of the function. When the context is done, the function stops running.
// - interval: The duration between each execution of the runners.
// - runners: A variadic parameter of Runner interfaces to be executed.
//
// The function uses a ticker to trigger the execution of the runners at the specified interval.
// If the context is done, the function stops and returns.
//
// Example usage:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	RunForever(ctx, time.Minute, runner1, runner2)
func RunForever(ctx context.Context, interval time.Duration, runners ...Runner) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, r := range runners {
				if err := r.Run(ctx); err != nil {
					logger.Errorf("Failed to run Runner %s: %v", r.Name(), err)
				}
			}
		}
	}
}
