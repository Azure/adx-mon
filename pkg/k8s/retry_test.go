package k8s

import (
	"context"
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MockObject implements client.Object for testing
type MockObject struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Spec   MockSpec   `json:"spec,omitempty"`
	Status MockStatus `json:"status,omitempty"`
}

type MockSpec struct {
	Field1 string `json:"field1,omitempty"`
	Field2 int    `json:"field2,omitempty"`
}

type MockStatus struct {
	Phase string `json:"phase,omitempty"`
}

func (m *MockObject) DeepCopyObject() runtime.Object {
	return &MockObject{
		TypeMeta:   m.TypeMeta,
		ObjectMeta: *m.ObjectMeta.DeepCopy(),
		Spec:       m.Spec,
		Status:     m.Status,
	}
}

// MockClient implements client.Client for testing
type MockClient struct {
	updateCallCount int
	getCallCount    int
	shouldConflict  []bool // Controls which update calls should return conflicts
	updateError     error  // Non-conflict error to return
	getError        error  // Error to return from Get calls
	getFunc         func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error

	// Object state simulation
	currentObject *MockObject
}

func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	m.getCallCount++

	if m.getFunc != nil {
		return m.getFunc(ctx, key, obj, opts...)
	}

	if m.getError != nil {
		return m.getError
	}

	// Simulate fetching the latest version
	mockObj, ok := obj.(*MockObject)
	if !ok {
		return fmt.Errorf("expected MockObject, got %T", obj)
	}

	if m.currentObject != nil {
		*mockObj = *m.currentObject
		// Simulate resource version increment
		mockObj.SetResourceVersion(fmt.Sprintf("v%d", m.getCallCount))
	}

	return nil
}

func (m *MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	m.updateCallCount++

	if m.updateError != nil {
		return m.updateError
	}

	// Check if this call should return a conflict
	if m.updateCallCount <= len(m.shouldConflict) && m.shouldConflict[m.updateCallCount-1] {
		return apierrors.NewConflict(
			schema.GroupResource{Group: "", Resource: "mockobjects"},
			obj.GetName(),
			fmt.Errorf("conflict on attempt %d", m.updateCallCount),
		)
	}

	// Successful update - store the object
	if mockObj, ok := obj.(*MockObject); ok {
		m.currentObject = &MockObject{
			TypeMeta:   mockObj.TypeMeta,
			ObjectMeta: *mockObj.ObjectMeta.DeepCopy(),
			Spec:       mockObj.Spec,
			Status:     mockObj.Status,
		}
	}

	return nil
}

// Implement the rest of client.Client interface (not used in tests)
func (m *MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return nil
}
func (m *MockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return nil
}
func (m *MockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return nil
}
func (m *MockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return nil
}
func (m *MockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return nil
}
func (m *MockClient) Status() client.StatusWriter { return nil }
func (m *MockClient) Scheme() *runtime.Scheme     { return nil }
func (m *MockClient) RESTMapper() meta.RESTMapper { return nil }
func (m *MockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, nil
}
func (m *MockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return true, nil
}
func (m *MockClient) SubResource(subResource string) client.SubResourceClient {
	return nil
}

func TestUpdateWithRetry_SuccessfulUpdate(t *testing.T) {
	ctx := context.Background()
	mockClient := &MockClient{}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
		Spec: MockSpec{
			Field1: "original",
			Field2: 42,
		},
	}

	// Test successful update on first try
	err := UpdateWithRetry(ctx, mockClient, obj, func(latest client.Object) error {
		latestMock := latest.(*MockObject)
		latestMock.Spec.Field1 = "updated"
		return nil
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if mockClient.updateCallCount != 1 {
		t.Errorf("Expected 1 update call, got: %d", mockClient.updateCallCount)
	}

	if mockClient.getCallCount != 0 {
		t.Errorf("Expected 0 get calls for successful update, got: %d", mockClient.getCallCount)
	}

	if obj.Spec.Field1 != "updated" {
		t.Errorf("Expected Field1 to be 'updated', got: %s", obj.Spec.Field1)
	}
}

func TestUpdateWithRetry_ConflictRetry(t *testing.T) {
	ctx := context.Background()
	mockClient := &MockClient{
		shouldConflict: []bool{true, true, false}, // Conflict on first 2 attempts, succeed on 3rd
		currentObject: &MockObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-object",
				Namespace: "default",
			},
			Spec: MockSpec{
				Field1: "server-version",
				Field2: 99,
			},
		},
	}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
		Spec: MockSpec{
			Field1: "client-version",
			Field2: 42,
		},
	}

	callCount := 0
	err := UpdateWithRetry(ctx, mockClient, obj, func(latest client.Object) error {
		callCount++
		latestMock := latest.(*MockObject)

		if callCount == 1 {
			// First call should have original client values
			if latestMock.Spec.Field1 != "client-version" {
				t.Errorf("Expected first call Field1 to be 'client-version', got: %s", latestMock.Spec.Field1)
			}
			if latestMock.Spec.Field2 != 42 {
				t.Errorf("Expected first call Field2 to be 42, got: %d", latestMock.Spec.Field2)
			}
		} else {
			// Retry calls should have server values
			if latestMock.Spec.Field1 != "server-version" {
				t.Errorf("Expected retry call Field1 to be 'server-version', got: %s", latestMock.Spec.Field1)
			}
			if latestMock.Spec.Field2 != 99 {
				t.Errorf("Expected retry call Field2 to be 99, got: %d", latestMock.Spec.Field2)
			}
		}

		// Apply our change to the latest version
		latestMock.Spec.Field1 = "updated-by-client"
		return nil
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if callCount != 3 {
		t.Errorf("Expected callback to be called 3 times, got: %d", callCount)
	}

	if mockClient.updateCallCount != 3 {
		t.Errorf("Expected 3 update calls, got: %d", mockClient.updateCallCount)
	}

	if mockClient.getCallCount != 2 {
		t.Errorf("Expected 2 get calls (for 2 retries), got: %d", mockClient.getCallCount)
	}

	// Verify the object was updated with the latest server state + our changes
	if obj.Spec.Field1 != "updated-by-client" {
		t.Errorf("Expected Field1 to be 'updated-by-client', got: %s", obj.Spec.Field1)
	}
	if obj.Spec.Field2 != 99 {
		t.Errorf("Expected Field2 to be 99 (from server), got: %d", obj.Spec.Field2)
	}
}

func TestUpdateWithRetry_MaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()
	mockClient := &MockClient{
		shouldConflict: []bool{true, true, true, true, true, true}, // Always conflict
	}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
	}

	err := UpdateWithRetry(ctx, mockClient, obj, func(latest client.Object) error {
		return nil
	})

	if err == nil {
		t.Fatal("Expected error due to max retries exceeded")
	}

	expectedError := "failed to update object after 5 retries due to conflicts"
	if err.Error() != expectedError {
		t.Errorf("Expected error message '%s', got: %s", expectedError, err.Error())
	}

	if mockClient.updateCallCount != 5 {
		t.Errorf("Expected 5 update calls, got: %d", mockClient.updateCallCount)
	}

	if mockClient.getCallCount != 5 {
		t.Errorf("Expected 5 get calls, got: %d", mockClient.getCallCount)
	}
}

func TestUpdateWithRetry_NonConflictError(t *testing.T) {
	ctx := context.Background()
	expectedError := errors.New("some other error")
	mockClient := &MockClient{
		updateError: expectedError,
	}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
	}

	err := UpdateWithRetry(ctx, mockClient, obj, func(latest client.Object) error {
		return nil
	})

	if err != expectedError {
		t.Errorf("Expected error to be %v, got: %v", expectedError, err)
	}

	if mockClient.updateCallCount != 1 {
		t.Errorf("Expected 1 update call, got: %d", mockClient.updateCallCount)
	}

	if mockClient.getCallCount != 0 {
		t.Errorf("Expected 0 get calls for non-conflict error, got: %d", mockClient.getCallCount)
	}
}

func TestUpdateWithRetry_GetError(t *testing.T) {
	ctx := context.Background()
	mockClient := &MockClient{
		shouldConflict: []bool{true}, // Trigger conflict to cause Get call
		getError:       errors.New("get failed"),
	}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
	}

	err := UpdateWithRetry(ctx, mockClient, obj, func(latest client.Object) error {
		return nil
	})

	if err == nil {
		t.Fatal("Expected error due to Get failure")
	}

	expectedError := "failed to fetch latest object for retry: get failed"
	if err.Error() != expectedError {
		t.Errorf("Expected error message '%s', got: %s", expectedError, err.Error())
	}
}

func TestUpdateWithRetry_ReapplyChangesError(t *testing.T) {
	ctx := context.Background()
	mockClient := &MockClient{}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
	}

	reapplyError := errors.New("reapply failed")
	err := UpdateWithRetry(ctx, mockClient, obj, func(latest client.Object) error {
		return reapplyError
	})

	if err == nil {
		t.Fatal("Expected error due to reapply failure")
	}

	expectedError := "failed to apply initial changes: reapply failed"
	if err.Error() != expectedError {
		t.Errorf("Expected error message '%s', got: %s", expectedError, err.Error())
	}
}

func TestUpdateWithRetry_ReapplyChangesErrorDuringRetry(t *testing.T) {
	ctx := context.Background()
	mockClient := &MockClient{
		shouldConflict: []bool{true}, // Trigger conflict to cause reapply
	}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
	}

	callCount := 0
	reapplyError := errors.New("reapply failed")
	err := UpdateWithRetry(ctx, mockClient, obj, func(latest client.Object) error {
		callCount++
		if callCount == 2 { // Fail on the second call (retry attempt)
			return reapplyError
		}
		return nil
	})

	if err == nil {
		t.Fatal("Expected error due to reapply failure")
	}

	expectedError := "failed to reapply changes: reapply failed"
	if err.Error() != expectedError {
		t.Errorf("Expected error message '%s', got: %s", expectedError, err.Error())
	}
}

func TestUpdateWithRetryPreserveSpec_Success(t *testing.T) {
	ctx := context.Background()
	mockClient := &MockClient{
		shouldConflict: []bool{true, false}, // Conflict once, then succeed
		currentObject: &MockObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "test-object",
				Namespace:       "default",
				ResourceVersion: "v1",
			},
			Spec: MockSpec{
				Field1: "server-field1",
				Field2: 999,
			},
			Status: MockStatus{
				Phase: "server-phase",
			},
		},
	}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
		Spec: MockSpec{
			Field1: "client-field1",
			Field2: 42,
		},
		Status: MockStatus{
			Phase: "client-phase",
		},
	}

	err := UpdateWithRetryPreserveSpec(ctx, mockClient, obj,
		func(o *MockObject) MockSpec { return o.Spec },
		func(o *MockObject, spec MockSpec) { o.Spec = spec },
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify that our spec was preserved but other fields came from server
	if obj.Spec.Field1 != "client-field1" {
		t.Errorf("Expected Field1 to be preserved as 'client-field1', got: %s", obj.Spec.Field1)
	}
	if obj.Spec.Field2 != 42 {
		t.Errorf("Expected Field2 to be preserved as 42, got: %d", obj.Spec.Field2)
	}
	if obj.Status.Phase != "server-phase" {
		t.Errorf("Expected Status.Phase to be from server 'server-phase', got: %s", obj.Status.Phase)
	}
	if obj.GetResourceVersion() != "v1" {
		t.Errorf("Expected ResourceVersion to be from server 'v1', got: %s", obj.GetResourceVersion())
	}
}

func TestUpdateWithRetryPreserveSpec_TypeMismatch(t *testing.T) {
	ctx := context.Background()
	mockClient := &MockClient{
		shouldConflict: []bool{true}, // Trigger conflict
		getFunc: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			// Return an error to simulate Get failure
			return fmt.Errorf("type assertion would fail")
		},
	}

	obj := &MockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
	}

	err := UpdateWithRetryPreserveSpec(ctx, mockClient, obj,
		func(o *MockObject) MockSpec { return o.Spec },
		func(o *MockObject, spec MockSpec) { o.Spec = spec },
	)

	if err == nil {
		t.Fatal("Expected error due to Get failure")
	}
}
