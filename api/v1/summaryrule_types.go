/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"encoding/json"
	"time"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
const (
	// SummaryRuleOwner is the owner of the summary rule
	SummaryRuleOwner = "summaryrule.adx-mon.azure.com"
	// SummaryRuleOperationIdOwner is the owner of the summary rule operation id
	SummaryRuleOperationIdOwner = "summaryrule.adx-mon.azure.com/OperationId"
	// SummaryRuleLastSuccessfulExecution tracks the end time of the last successful query execution
	SummaryRuleLastSuccessfulExecution = "summaryrule.adx-mon.azure.com/LastSuccessfulExecution"
	// SummaryRuleAsyncOperationPollInterval acts as a cooldown period between checking
	// the status of an async operation. This value is somewhat arbitrary, but the intent
	// is to not overwhelm the service with requests.
	SummaryRuleAsyncOperationPollInterval = 10 * time.Minute
)

// SummaryRuleSpec defines the desired state of SummaryRule
type SummaryRuleSpec struct {
	// Database is the name of the database in which the function will be created
	Database string `json:"database"`
	// Table is rule output destination
	Table string `json:"table"`
	// Name is the name of the rule
	Name string `json:"name"`
	// Body is the KQL body of the function
	Body string `json:"body"`
	// Interval is the cadence at which the rule will be executed
	Interval metav1.Duration `json:"interval"`

	// Key/Value pairs used to determine when a summary rule can execute. If empty, always execute. Keys and values
	// are deployment specific and configured on ingestor instances. For example, an ingestor instance may be
	// started with `--cluster-labels=region=eastus`. If a SummaryRule has `criteria: {region: [eastus]}`, then the rule will only
	// execute on that ingestor. Any key/values pairs must match (case-insensitive) for the rule to execute.
	Criteria map[string][]string `json:"criteria,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SummaryRule is the Schema for the summaryrules API
type SummaryRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SummaryRuleSpec   `json:"spec,omitempty"`
	Status SummaryRuleStatus `json:"status,omitempty"`
}

// SummaryRuleStatus defines the observed state of Function
type SummaryRuleStatus struct {
	// Conditions is an array of current observed SummaryRule conditions
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (s *SummaryRule) GetCondition() *metav1.Condition {
	return meta.FindStatusCondition(s.Status.Conditions, SummaryRuleOwner)
}

func (s *SummaryRule) GetLastSuccessfulExecutionTime() *time.Time {
	condition := meta.FindStatusCondition(s.Status.Conditions, SummaryRuleLastSuccessfulExecution)
	if condition == nil {
		return nil
	}

	// Parse the time from the message field
	if condition.Message == "" {
		return nil
	}

	t, err := time.Parse(time.RFC3339Nano, condition.Message)
	if err != nil {
		return nil
	}

	return &t
}

func (s *SummaryRule) SetLastSuccessfulExecutionTime(endTime time.Time) {
	condition := &metav1.Condition{
		Type:               SummaryRuleLastSuccessfulExecution,
		Status:             metav1.ConditionTrue,
		Reason:             "ExecutionCompleted",
		Message:            endTime.UTC().Format(time.RFC3339Nano),
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: s.GetGeneration(),
	}

	meta.SetStatusCondition(&s.Status.Conditions, *condition)
}

func (s *SummaryRule) SetCondition(c metav1.Condition) {
	if c.ObservedGeneration == 0 {
		c.Reason = "Created"
	} else {
		c.Reason = "Updated"
	}
	if c.Status == metav1.ConditionFalse {
		c.Reason = "Failed"
	}
	c.ObservedGeneration = s.GetGeneration()
	c.Type = SummaryRuleOwner

	meta.SetStatusCondition(&s.Status.Conditions, c)
}

// AsyncOperation represents a serialized async operation.
// We store AsyncOperations in a Condition's Message field.
// Each Message field has a max length of 32768.
// Our array of AsyncOperations, with a single entry:
// [{"operationId": "9028e8b7-7350-4870-a0e7-ab3538049876", "startTime": "2025-03-12T14:48:23.8612789Z", "endTime": "2025-03-12T14:48:23.8612789Z"}]
// So we can hold 236 entries. For safety, we'll limit it to 200.
// We only store incomplete AsyncOperations, so they're
// often pruned; however, if we reach the maximum capacity,
// we'll remove the oldest AsyncOperation.
type AsyncOperation struct {
	OperationId string `json:"operationId"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
}

func (s *SummaryRule) GetAsyncOperations() []AsyncOperation {
	condition := meta.FindStatusCondition(s.Status.Conditions, SummaryRuleOperationIdOwner)
	if condition == nil {
		return nil
	}

	var asyncOperations []AsyncOperation
	if err := json.Unmarshal([]byte(condition.Message), &asyncOperations); err != nil {
		// If we can't unmarshal the JSON, return an empty slice, something has gone wrong
		// with the CRD and it's not going to get better if we try later, we need to start
		// over with the condition.
		return nil
	}

	return asyncOperations
}

func (s *SummaryRule) SetAsyncOperation(operation AsyncOperation) {
	asyncOperations := s.GetAsyncOperations()

	// Check if the operation already exists
	found := false
	for i, op := range asyncOperations {
		// If we're unable to submit an AsyncOperation, we add it to our backlog for
		// future submission. Once we're able to submit the operation, we set the
		// operation-id, which means we need to detect this case and match operations
		// based on their time windows.
		if op.OperationId == "" {
			if op.StartTime == operation.StartTime &&
				op.EndTime == operation.EndTime &&
				op.StartTime != "" && op.EndTime != "" {
				// If the operation is in the backlog, we need to update it with the new
				// operation-id and the start and end times.
				asyncOperations[i] = operation
				found = true
				break
			}
		} else if op.OperationId == operation.OperationId {
			// Update the existing operation
			asyncOperations[i] = operation
			found = true
			break
		}
	}
	// If the operation doesn't exist, append it
	if !found {
		asyncOperations = append(asyncOperations, operation)
	}
	// Limit the number of async operations to 200
	if len(asyncOperations) > 200 {
		asyncOperations = asyncOperations[1:]
	}
	// Marshal the async operations back to JSON
	operationsJSON, err := json.Marshal(asyncOperations)
	if err != nil {
		// If we can't marshal the JSON, something has gone horribly wrong
		// and trying again later isn't going to yield a different result.
	}
	// Set the condition message to the JSON string
	condition := meta.FindStatusCondition(s.Status.Conditions, SummaryRuleOperationIdOwner)
	if condition == nil {
		condition = &metav1.Condition{}
	}
	condition.Message = string(operationsJSON)
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = s.GetGeneration()
	condition.Type = SummaryRuleOperationIdOwner
	condition.Status = metav1.ConditionUnknown
	condition.Reason = "InProgress"

	meta.SetStatusCondition(&s.Status.Conditions, *condition)
}

func (s *SummaryRule) RemoveAsyncOperation(operationId string) {
	asyncOperations := s.GetAsyncOperations()

	// Remove the operation with the given ID
	for i, op := range asyncOperations {
		if op.OperationId == operationId {
			asyncOperations = append(asyncOperations[:i], asyncOperations[i+1:]...)
			break
		}
	}

	// Marshal the async operations back to JSON
	operationsJSON, err := json.Marshal(asyncOperations)
	if err != nil {
		// If we can't marshal the JSON, something has gone horribly wrong
		// and trying again later isn't going to yield a different result.
	}

	// Set the condition message to the JSON string
	condition := meta.FindStatusCondition(s.Status.Conditions, SummaryRuleOperationIdOwner)
	if condition == nil {
		condition = &metav1.Condition{}
	}
	condition.Message = string(operationsJSON)
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = s.GetGeneration()
	condition.Type = SummaryRuleOperationIdOwner

	if len(asyncOperations) == 0 {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Complete"
	} else {
		condition.Status = metav1.ConditionUnknown
		condition.Reason = "InProgress"
	}

	meta.SetStatusCondition(&s.Status.Conditions, *condition)
}

// +kubebuilder:object:root=true

// SummaryRuleList contains a list of SummaryRule
type SummaryRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SummaryRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SummaryRule{}, &SummaryRuleList{})
}
