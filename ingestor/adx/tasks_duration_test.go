package adx

import (
	"context"
	"errors"
	"testing"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Mock storage that can simulate duration parsing errors
type mockStorageWithDurationError struct {
	storage.CRDHandler
	shouldError bool
}

func (m *mockStorageWithDurationError) List(ctx context.Context, list client.ObjectList, filters ...storage.ListFilterFunc) error {
	if m.shouldError {
		// Simulate the error that occurs when a SummaryRule has an invalid duration field
		return errors.New("failed to list CRDs: time: unknown unit \"something-that-is-not-a-valid-go-duration\" in duration \"something-that-is-not-a-valid-go-duration\"")
	}
	// Return empty list for successful case
	if summaryRules, ok := list.(*v1.SummaryRuleList); ok {
		summaryRules.Items = []v1.SummaryRule{}
	}
	return nil
}

func (m *mockStorageWithDurationError) UpdateStatus(ctx context.Context, obj client.Object, errStatus error) error {
	return nil
}

func (m *mockStorageWithDurationError) UpdateStatusWithKustoErrorParsing(ctx context.Context, obj client.Object, errStatus error) error {
	return nil
}

func TestSummaryRuleTask_ResilienceToInvalidDurations(t *testing.T) {
	tests := []struct {
		name        string
		shouldError bool
		expectError bool
	}{
		{
			name:        "successful list operation",
			shouldError: false,
			expectError: false,
		},
		{
			name:        "duration parsing error should be handled gracefully", 
			shouldError: true,
			expectError: false, // Should not return error, should handle gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStorage := &mockStorageWithDurationError{shouldError: tt.shouldError}
			
			task := &SummaryRuleTask{
				store:         mockStorage,
				ClusterLabels: map[string]string{"region": "test"},
			}
			
			// Mock the GetOperations and SubmitRule functions
			task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
				return []AsyncOperationStatus{}, nil
			}
			task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
				return "test-operation-id", nil
			}

			err := task.Run(context.Background())
			
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			
			// If this was a duration error case, verify it was handled gracefully
			if tt.shouldError && !tt.expectError {
				// The task should have logged the error but continued execution
				// Since we can't easily capture logs in this test, we just verify no error was returned
				if err != nil {
					t.Errorf("Duration parsing errors should be handled gracefully, but got error: %v", err)
				}
			}
		})
	}
}

func TestSummaryRuleTask_initializeRun_DurationErrorHandling(t *testing.T) {
	mockStorage := &mockStorageWithDurationError{shouldError: true}
	
	task := &SummaryRuleTask{
		store:         mockStorage,
		ClusterLabels: map[string]string{"region": "test"},
	}
	
	// Mock the GetOperations function
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return []AsyncOperationStatus{}, nil
	}

	summaryRules, operations, err := task.initializeRun(context.Background())
	
	// Should not return an error
	if err != nil {
		t.Errorf("initializeRun should handle duration errors gracefully, but got error: %v", err)
	}
	
	// Should return empty list when duration errors occur
	if summaryRules == nil {
		t.Errorf("Expected non-nil summary rules list")
	}
	
	if len(summaryRules.Items) != 0 {
		t.Errorf("Expected empty summary rules list when duration error occurs, got %d items", len(summaryRules.Items))
	}
	
	// Operations should still be returned
	if operations == nil {
		t.Errorf("Expected non-nil operations list")
	}
}