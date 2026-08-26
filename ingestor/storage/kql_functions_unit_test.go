package storage

import (
	"context"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFunctionsUpdateStatusRetriesConflict(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	newFunction := func() *adxmonv1.Function {
		return &adxmonv1.Function{
			ObjectMeta: metav1.ObjectMeta{Name: "fn", Namespace: "default", Generation: 2},
			Spec:       adxmonv1.FunctionSpec{Database: "db", Body: "print 1"},
		}
	}

	t.Run("retries with latest resource version", func(t *testing.T) {
		fn := newFunction()
		base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&adxmonv1.Function{}).WithObjects(fn).Build()
		wrapped := &conflictFunctionClient{Client: base, mode: conflictOnce}
		store := NewFunctions(wrapped, nil)

		desired := fn.DeepCopy()
		desired.Status.Status = adxmonv1.Failed
		desired.Status.Reason = "CriteriaNotMatched"
		desired.Status.ObservedGeneration = desired.Generation
		require.NoError(t, store.UpdateStatus(ctx, desired))
		require.Equal(t, 2, wrapped.statusUpdates)

		actual := &adxmonv1.Function{}
		require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(fn), actual))
		require.Equal(t, desired.Status.Reason, actual.Status.Reason)
		require.Equal(t, desired.Generation, actual.Status.ObservedGeneration)
		require.Equal(t, fn.Spec, actual.Spec)
	})

	t.Run("converges when another writer persisted the same status", func(t *testing.T) {
		fn := newFunction()
		base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&adxmonv1.Function{}).WithObjects(fn).Build()
		wrapped := &conflictFunctionClient{Client: base, mode: persistThenConflict}
		store := NewFunctions(wrapped, nil)

		desired := fn.DeepCopy()
		desired.Status.Status = adxmonv1.Failed
		desired.Status.Reason = "CriteriaNotMatched"
		desired.Status.ObservedGeneration = desired.Generation
		require.NoError(t, store.UpdateStatus(ctx, desired))
		require.Equal(t, 1, wrapped.statusUpdates)
	})

	t.Run("discards status calculated from an older generation", func(t *testing.T) {
		fn := newFunction()
		base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&adxmonv1.Function{}).WithObjects(fn).Build()
		wrapped := &conflictFunctionClient{Client: base, mode: advanceGenerationThenConflict}
		store := NewFunctions(wrapped, nil)

		desired := fn.DeepCopy()
		desired.Status.Status = adxmonv1.Failed
		desired.Status.Reason = "CriteriaNotMatched"
		desired.Status.ObservedGeneration = desired.Generation
		require.NoError(t, store.UpdateStatus(ctx, desired))
		require.Equal(t, 1, wrapped.statusUpdates)

		actual := &adxmonv1.Function{}
		require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(fn), actual))
		require.Equal(t, int64(3), actual.Generation)
		require.Empty(t, actual.Status.Reason)
		require.Zero(t, actual.Status.ObservedGeneration)
	})
}

func TestFunctionsUpdateStatusRetriesFinalizerConflict(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	deletionTime := metav1.NewTime(time.Now())
	fn := &adxmonv1.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "fn",
			Namespace:         "default",
			Generation:        2,
			DeletionTimestamp: &deletionTime,
			Finalizers:        []string{FinalizerName},
		},
		Spec: adxmonv1.FunctionSpec{Database: "db", Body: "print 1"},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&adxmonv1.Function{}).WithObjects(fn).Build()
	wrapped := &finalizerConflictClient{Client: base}
	store := NewFunctions(wrapped, nil)

	desired := fn.DeepCopy()
	desired.Status.Status = adxmonv1.Success
	require.NoError(t, store.UpdateStatus(ctx, desired))
	require.Equal(t, 2, wrapped.updates)
	require.NotNil(t, wrapped.updated)
	require.NotContains(t, wrapped.updated.Finalizers, FinalizerName)
	require.Equal(t, "preserved", wrapped.updated.Annotations["concurrent-update"])
}

type conflictMode int

const (
	conflictOnce conflictMode = iota
	persistThenConflict
	advanceGenerationThenConflict
)

type conflictFunctionClient struct {
	client.Client
	mode          conflictMode
	statusUpdates int
}

func (c *conflictFunctionClient) Status() client.SubResourceWriter {
	return &conflictStatusWriter{SubResourceWriter: c.Client.Status(), client: c}
}

type conflictStatusWriter struct {
	client.SubResourceWriter
	client *conflictFunctionClient
}

type finalizerConflictClient struct {
	client.Client
	updates int
	updated *adxmonv1.Function
}

func (c *finalizerConflictClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updates++
	if c.updates == 1 {
		latest := &adxmonv1.Function{}
		key := client.ObjectKeyFromObject(obj)
		if err := c.Client.Get(ctx, key, latest); err != nil {
			return err
		}
		latest.Annotations = map[string]string{"concurrent-update": "preserved"}
		if err := c.Client.Update(ctx, latest); err != nil {
			return err
		}
		return apierrors.NewConflict(schema.GroupResource{Group: "adx-mon.azure.com", Resource: "functions"}, obj.GetName(), nil)
	}
	c.updated = obj.DeepCopyObject().(*adxmonv1.Function)
	return c.Client.Update(ctx, obj, opts...)
}

func (w *conflictStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	w.client.statusUpdates++
	if w.client.statusUpdates != 1 {
		return w.SubResourceWriter.Update(ctx, obj, opts...)
	}

	switch w.client.mode {
	case persistThenConflict:
		if err := w.client.Client.Status().Update(ctx, obj); err != nil {
			return err
		}
	case advanceGenerationThenConflict:
		latest := &adxmonv1.Function{}
		key := client.ObjectKeyFromObject(obj)
		if err := w.client.Client.Get(ctx, key, latest); err != nil {
			return err
		}
		latest.Generation++
		latest.Spec.Body = "print 2"
		if err := w.client.Client.Update(ctx, latest); err != nil {
			return err
		}
	}

	return apierrors.NewConflict(schema.GroupResource{Group: "adx-mon.azure.com", Resource: "functions"}, obj.GetName(), nil)
}
