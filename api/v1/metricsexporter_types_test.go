package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMetricsExporter_GetLastExecutionTime(t *testing.T) {
	t.Run("no condition returns nil", func(t *testing.T) {
		me := &MetricsExporter{}
		require.Nil(t, me.GetLastExecutionTime())
	})

	t.Run("condition without message returns nil", func(t *testing.T) {
		me := &MetricsExporter{
			Status: MetricsExporterStatus{
				Conditions: []metav1.Condition{
					{
						Type:   MetricsExporterLastSuccessfulExecution,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
		require.Nil(t, me.GetLastExecutionTime())
	})

	t.Run("valid time in condition message", func(t *testing.T) {
		testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		me := &MetricsExporter{
			Status: MetricsExporterStatus{
				Conditions: []metav1.Condition{
					{
						Type:    MetricsExporterLastSuccessfulExecution,
						Status:  metav1.ConditionTrue,
						Message: testTime.Format(time.RFC3339Nano),
					},
				},
			},
		}
		result := me.GetLastExecutionTime()
		require.NotNil(t, result)
		require.Equal(t, testTime, *result)
	})

	t.Run("invalid time format returns nil", func(t *testing.T) {
		me := &MetricsExporter{
			Status: MetricsExporterStatus{
				Conditions: []metav1.Condition{
					{
						Type:    MetricsExporterLastSuccessfulExecution,
						Status:  metav1.ConditionTrue,
						Message: "invalid-time-format",
					},
				},
			},
		}
		require.Nil(t, me.GetLastExecutionTime())
	})
}

func TestMetricsExporter_SetLastExecutionTime(t *testing.T) {
	testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	me := &MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 2,
		},
	}

	me.SetLastExecutionTime(testTime)

	// Check that the condition was set
	condition := meta.FindStatusCondition(me.Status.Conditions, MetricsExporterLastSuccessfulExecution)
	require.NotNil(t, condition)
	require.Equal(t, MetricsExporterLastSuccessfulExecution, condition.Type)
	require.Equal(t, metav1.ConditionTrue, condition.Status)
	require.Equal(t, "ExecutionCompleted", condition.Reason)
	require.Equal(t, testTime.UTC().Format(time.RFC3339Nano), condition.Message)
	require.Equal(t, int64(2), condition.ObservedGeneration)

	// Check that GetLastExecutionTime returns the same time
	result := me.GetLastExecutionTime()
	require.NotNil(t, result)
	require.Equal(t, testTime, *result)
}

func TestMetricsExporter_ShouldExecuteQuery(t *testing.T) {
	fixedTime := time.Date(2023, 1, 1, 14, 30, 0, 0, time.UTC)
	fakeClock := NewFakeClock(fixedTime)

	t.Run("first execution - time passed", func(t *testing.T) {
		me := &MetricsExporter{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
			Status: MetricsExporterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               MetricsExporterOwner,
						ObservedGeneration: 1,
						LastTransitionTime: metav1.Time{Time: fixedTime.Add(-10 * time.Minute)}, // 10 minutes ago
					},
				},
			},
		}
		require.True(t, me.ShouldExecuteQuery(fakeClock))
	})

	t.Run("first execution - time not passed", func(t *testing.T) {
		me := &MetricsExporter{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 15 * time.Minute},
			},
			Status: MetricsExporterStatus{
				Conditions: []metav1.Condition{
					{
						Type:               MetricsExporterOwner,
						ObservedGeneration: 1,
						LastTransitionTime: metav1.Time{Time: fixedTime.Add(-10 * time.Minute)}, // 10 minutes ago
					},
				},
			},
		}
		require.False(t, me.ShouldExecuteQuery(fakeClock))
	})

	t.Run("subsequent execution - time passed", func(t *testing.T) {
		me := &MetricsExporter{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		}
		me.SetLastExecutionTime(fixedTime.Add(-10 * time.Minute)) // 10 minutes ago

		// Set up the main condition with observed generation matching current
		mainCondition := &metav1.Condition{
			Type:               MetricsExporterOwner,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 1, // Matches current generation
			LastTransitionTime: metav1.Time{Time: fixedTime.Add(-15 * time.Minute)},
		}
		meta.SetStatusCondition(&me.Status.Conditions, *mainCondition)

		require.True(t, me.ShouldExecuteQuery(fakeClock))
	})

	t.Run("subsequent execution - time not passed", func(t *testing.T) {
		me := &MetricsExporter{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		}
		me.SetLastExecutionTime(fixedTime.Add(-3 * time.Minute)) // 3 minutes ago

		// Set up the main condition with observed generation matching current
		mainCondition := &metav1.Condition{
			Type:               MetricsExporterOwner,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 1, // Matches current generation
			LastTransitionTime: metav1.Time{Time: fixedTime.Add(-10 * time.Minute)},
		}
		meta.SetStatusCondition(&me.Status.Conditions, *mainCondition)

		require.False(t, me.ShouldExecuteQuery(fakeClock))
	})

	t.Run("generation changed - should execute", func(t *testing.T) {
		me := &MetricsExporter{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 2, // Changed generation
			},
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		}
		me.SetLastExecutionTime(fixedTime.Add(-1 * time.Minute)) // 1 minute ago

		// Set up the main condition with old observed generation
		mainCondition := &metav1.Condition{
			Type:               MetricsExporterOwner,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 1, // Old generation
			LastTransitionTime: metav1.Time{Time: fixedTime.Add(-5 * time.Minute)},
		}
		meta.SetStatusCondition(&me.Status.Conditions, *mainCondition)

		require.True(t, me.ShouldExecuteQuery(fakeClock))
	})

	t.Run("deletion timestamp set - should execute", func(t *testing.T) {
		now := metav1.NewTime(fixedTime)
		me := &MetricsExporter{
			ObjectMeta: metav1.ObjectMeta{
				Generation:        1,
				DeletionTimestamp: &now,
			},
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		}
		me.SetLastExecutionTime(fixedTime.Add(-1 * time.Minute)) // 1 minute ago

		// Set observed generation
		condition := meta.FindStatusCondition(me.Status.Conditions, MetricsExporterLastSuccessfulExecution)
		condition.ObservedGeneration = 1
		meta.SetStatusCondition(&me.Status.Conditions, *condition)

		require.True(t, me.ShouldExecuteQuery(fakeClock))
	})

	t.Run("no conditions - first execution timing", func(t *testing.T) {
		me := &MetricsExporter{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		}
		// No conditions set, should use current time - interval for calculation
		require.True(t, me.ShouldExecuteQuery(fakeClock))
	})
}

func TestMetricsExporter_NextExecutionWindow(t *testing.T) {
	fixedTime := time.Date(2023, 1, 1, 14, 30, 45, 0, time.UTC)
	fakeClock := NewFakeClock(fixedTime)

	t.Run("first execution - 5 minute interval", func(t *testing.T) {
		me := &MetricsExporter{
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		}
		startTime, endTime := me.NextExecutionWindow(fakeClock)

		expectedStartTime := time.Date(2023, 1, 1, 14, 25, 0, 0, time.UTC) // Aligned and went back one interval
		expectedEndTime := time.Date(2023, 1, 1, 14, 30, 0, 0, time.UTC)   // Aligned to current time

		require.Equal(t, expectedStartTime, startTime)
		require.Equal(t, expectedEndTime, endTime)
	})

	t.Run("first execution - 1 hour interval", func(t *testing.T) {
		me := &MetricsExporter{
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 1 * time.Hour},
			},
		}
		startTime, endTime := me.NextExecutionWindow(fakeClock)

		expectedStartTime := time.Date(2023, 1, 1, 13, 0, 0, 0, time.UTC) // Aligned and went back one interval
		expectedEndTime := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC)   // Aligned to current time

		require.Equal(t, expectedStartTime, startTime)
		require.Equal(t, expectedEndTime, endTime)
	})

	t.Run("subsequent execution", func(t *testing.T) {
		me := &MetricsExporter{
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 15 * time.Minute},
			},
		}
		lastExecutionTime := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC)
		me.SetLastExecutionTime(lastExecutionTime)

		startTime, endTime := me.NextExecutionWindow(fakeClock)

		expectedStartTime := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC) // From last execution time
		expectedEndTime := time.Date(2023, 1, 1, 14, 15, 0, 0, time.UTC)  // Plus interval

		require.Equal(t, expectedStartTime, startTime)
		require.Equal(t, expectedEndTime, endTime)
	})

	t.Run("subsequent execution - don't go into future", func(t *testing.T) {
		me := &MetricsExporter{
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 2 * time.Hour},
			},
		}
		lastExecutionTime := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC)
		me.SetLastExecutionTime(lastExecutionTime)

		startTime, endTime := me.NextExecutionWindow(fakeClock)

		expectedStartTime := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC) // From last execution time
		expectedEndTime := time.Date(2023, 1, 1, 14, 30, 0, 0, time.UTC)  // Capped at current time (minute aligned)

		require.Equal(t, expectedStartTime, startTime)
		require.Equal(t, expectedEndTime, endTime)
	})

	t.Run("nil clock defaults to real clock", func(t *testing.T) {
		me := &MetricsExporter{
			Spec: MetricsExporterSpec{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		}
		// Should not panic
		startTime, endTime := me.NextExecutionWindow(nil)
		require.False(t, startTime.IsZero())
		require.False(t, endTime.IsZero())
		require.True(t, endTime.After(startTime))
	})
}
