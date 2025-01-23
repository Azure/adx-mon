package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/scheduler"
	"github.com/stretchr/testify/require"
)

func TestSchedulerOpenAndClose(t *testing.T) {
	elector := &FakeElector{}
	scheduler := scheduler.NewScheduler(elector)
	ctx := context.Background()

	require.NoError(t, scheduler.Open(ctx))
	require.NoError(t, scheduler.Close())
}

func TestScheduleEveryRunsFunction(t *testing.T) {
	elector := &FakeElector{isLeader: true}
	scheduler := scheduler.NewScheduler(elector)
	ctx := context.Background()
	scheduler.Open(ctx)
	defer scheduler.Close()

	called := make(chan struct{})
	scheduler.ScheduleEvery(10*time.Millisecond, "test-task", func(ctx context.Context) error {
		called <- struct{}{}
		return nil
	})

	time.Sleep(20 * time.Millisecond)

	select {
	case <-called:
	default:
		require.Fail(t, "function was not called")
	}
}

func TestScheduleEverySkipsIfNotLeader(t *testing.T) {
	elector := &FakeElector{isLeader: false}
	scheduler := scheduler.NewScheduler(elector)
	ctx := context.Background()
	scheduler.Open(ctx)
	defer scheduler.Close()

	called := make(chan struct{})
	scheduler.ScheduleEvery(10*time.Millisecond, "test-task", func(ctx context.Context) error {
		called <- struct{}{}
		return nil
	})

	time.Sleep(20 * time.Millisecond)
	select {
	case <-called:
		require.Fail(t, "function was called")
	default:
	}
}

func TestScheduleEveryLogsError(t *testing.T) {
	elector := &FakeElector{isLeader: true}
	scheduler := scheduler.NewScheduler(elector)
	ctx := context.Background()
	scheduler.Open(ctx)
	defer scheduler.Close()

	scheduler.ScheduleEvery(10*time.Millisecond, "test-task", func(ctx context.Context) error {
		return errors.New("test error")
	})

	// Assuming logger.Errorf logs to a buffer or mock logger
	time.Sleep(20 * time.Millisecond)
}

type FakeElector struct {
	isLeader bool
}

func (m *FakeElector) IsLeader() bool {
	return m.isLeader
}
