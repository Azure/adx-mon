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
	"sort"
	"time"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
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
	// SummaryRuleMaxBackfillLookback is the maximum allowed lookback period for backfill operations (30 days)
	SummaryRuleMaxBackfillLookback = 30 * 24 * time.Hour // 30 days
)

// BackfillSpec defines the configuration for backfilling historical data
type BackfillSpec struct {
	// Start is the current backfill position (RFC3339 format)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	Start string `json:"start"`

	// End is the target end time for backfill (RFC3339 format)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	End string `json:"end"`

	// OperationId is the current active Kusto operation ID for this backfill window
	// +optional
	OperationId string `json:"operationId,omitempty"`
}

// SummaryRuleSpec defines the desired state of SummaryRule
type SummaryRuleSpec struct {
	// Database is the name of the database in which the function will be created
	Database string `json:"database"`
	// Table is rule output destination
	Table string `json:"table"`
	// Name is the name of the rule (deprecated and not used - use `metadata.name` instead)
	Name string `json:"name,omitempty"`
	// Body is the KQL body of the function
	Body string `json:"body"`
	// Interval is the cadence at which the rule will be executed
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="interval must be a valid positive duration"
	Interval metav1.Duration `json:"interval"`
	// IngestionDelay is the delay to subtract from the execution window start and end times
	// to account for data ingestion latency. This ensures the query processes data that has
	// been fully ingested. If not specified, no delay is applied.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == '' || duration(self) >= duration('0s')",message="ingestionDelay must be a valid duration"
	IngestionDelay *metav1.Duration `json:"ingestionDelay,omitempty"`

	// Key/Value pairs used to determine when a summary rule can execute. If empty, always execute. Keys and values
	// are deployment specific and configured on ingestor instances. For example, an ingestor instance may be
	// started with `--cluster-labels=region=eastus`. If a SummaryRule has `criteria: {region: [eastus]}`, then the rule will only
	// execute on that ingestor. Any key/values pairs must match (case-insensitive) for the rule to execute.
	Criteria map[string][]string `json:"criteria,omitempty"`

	// Backfill specifies historical data processing configuration. When specified, the rule will process
	// historical data from the start datetime to the end datetime in addition to normal interval execution.
	// The system will automatically advance through time windows and remove this field when complete.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!has(self) || (has(self.start) && has(self.end))",message="backfill start and end are required when backfill is specified"
	Backfill *BackfillSpec `json:"backfill,omitempty"`
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

func (s *SummaryRule) GetLastExecutionTime() *time.Time {
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

func (s *SummaryRule) SetLastExecutionTime(endTime time.Time) {
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

func (s *SummaryRule) ShouldSubmitRule(clk clock.Clock) bool {
	clk = s.ensureClockIsSet(clk)

	lastSuccessfulEndTime := s.GetLastExecutionTime()
	cnd := s.GetCondition()
	if cnd == nil {
		// For first-time execution, initialize the condition with a timestamp
		// that's one interval back from current time
		cnd = &metav1.Condition{
			LastTransitionTime: metav1.Time{Time: clk.Now().Add(-s.Spec.Interval.Duration)},
		}
	}
	// Determine if the rule should be executed based on several criteria:
	// 1. The rule is being deleted
	// 2. Rule has been updated (new generation)
	// 3. It's time for the next interval execution (based on actual time windows)
	return s.DeletionTimestamp != nil || // Rule is being deleted
		cnd.ObservedGeneration != s.GetGeneration() || // A new version of this CRD was created
		(lastSuccessfulEndTime != nil && clk.Since(*lastSuccessfulEndTime) >= s.Spec.Interval.Duration) || // Time for next interval
		(lastSuccessfulEndTime == nil && clk.Since(cnd.LastTransitionTime.Time) >= s.Spec.Interval.Duration) // First execution timing
}

// calculateTimeWindow handles core window calculation logic for backfill operations.
//
// IMPORTANT: This method intentionally does NOT share logic with NextExecutionWindow()
// despite apparent similarities. The methods implement fundamentally different approaches
// to ingestion delay handling:
//
// calculateTimeWindow (this method):
//   - Applies ingestion delay AFTER interval alignment
//   - Calculates clean interval boundaries first: startPos.truncate(interval)
//   - Then shifts the entire window: window.add(-delay)
//   - Maintains consistent time boundaries across backfill operations
//
// NextExecutionWindow (normal execution):
//   - Applies ingestion delay BEFORE interval alignment
//   - Uses delay to determine the alignment point itself
//   - Creates execution windows that are "shifted" by the delay amount
//
// These represent two different architectural approaches that cannot be unified without
// breaking existing behavior. See NextExecutionWindow() comment for more details.
func (s *SummaryRule) calculateTimeWindow(clk clock.Clock, startPosition time.Time, maxEndTime *time.Time) (time.Time, time.Time) {
	clk = s.ensureClockIsSet(clk)

	delay := s.getIngestionDelay()
	windowStartTime, windowEndTime := s.calculateIntervalBoundaries(startPosition)
	windowEndTime = s.applyBoundaryConstraints(windowEndTime, maxEndTime)

	return s.applyIngestionDelayToWindow(windowStartTime, windowEndTime, delay)
}

// getIngestionDelay returns the configured ingestion delay duration, or zero if not set
func (s *SummaryRule) getIngestionDelay() time.Duration {
	if s.Spec.IngestionDelay != nil {
		return s.Spec.IngestionDelay.Duration
	}
	return 0
}

// calculateIntervalBoundaries aligns the start position to interval boundaries
func (s *SummaryRule) calculateIntervalBoundaries(startPosition time.Time) (time.Time, time.Time) {
	intervalDuration := s.Spec.Interval.Duration
	windowStartTime := startPosition.UTC().Truncate(intervalDuration)
	windowEndTime := windowStartTime.Add(intervalDuration)
	return windowStartTime, windowEndTime
}

// applyBoundaryConstraints ensures the window end doesn't exceed the maximum end time
func (s *SummaryRule) applyBoundaryConstraints(windowEndTime time.Time, maxEndTime *time.Time) time.Time {
	if maxEndTime != nil && windowEndTime.After(*maxEndTime) {
		return *maxEndTime
	}
	return windowEndTime
}

// applyIngestionDelayToWindow shifts both start and end times by the ingestion delay
func (s *SummaryRule) applyIngestionDelayToWindow(windowStartTime, windowEndTime time.Time, delay time.Duration) (time.Time, time.Time) {
	return windowStartTime.Add(-delay), windowEndTime.Add(-delay)
}

// ensureClockIsSet returns a real clock if the provided clock is nil
func (s *SummaryRule) ensureClockIsSet(clk clock.Clock) clock.Clock {
	if clk == nil {
		return clock.RealClock{}
	}
	return clk
}

// capWindowEndToCurrentTime ensures the window end doesn't exceed current time (for normal execution)
func (s *SummaryRule) capWindowEndToCurrentTime(clk clock.Clock, windowEndTime time.Time, delay time.Duration) time.Time {
	now := clk.Now().UTC().Add(-delay).Truncate(time.Minute)
	if windowEndTime.After(now) {
		return now
	}
	return windowEndTime
}

// NextExecutionWindow calculates the time window for normal interval-based execution.
//
// IMPORTANT: This method intentionally does NOT share logic with calculateTimeWindow()
// because they implement fundamentally different approaches to ingestion delay handling:
//
// NextExecutionWindow (this method):
//   - Applies ingestion delay BEFORE interval alignment
//   - For first execution: (currentTime - delay).truncate(interval) = endTime
//   - For subsequent: (lastEndTime - delay).truncate(interval) = startTime
//   - This creates execution windows that are "shifted" by the delay amount
//
// calculateTimeWindow (backfill method):
//   - Applies ingestion delay AFTER interval alignment
//   - Calculates clean interval boundaries first, then shifts the entire window
//   - This maintains consistent time window boundaries across backfill operations
//
// These represent two different time calculation strategies that serve different purposes
// and cannot be unified without breaking existing behavior. Any attempt to share this
// logic will break the comprehensive test suite that validates ingestion delay behavior.
func (s *SummaryRule) NextExecutionWindow(clk clock.Clock) (windowStartTime time.Time, windowEndTime time.Time) {
	clk = s.ensureClockIsSet(clk)
	delay := s.getIngestionDelay()

	lastSuccessfulEndTime := s.GetLastExecutionTime()
	if lastSuccessfulEndTime == nil {
		// First execution: start from current time minus delay, aligned to interval boundary
		now := clk.Now().UTC().Add(-delay)
		alignedNow := now.Truncate(s.Spec.Interval.Duration)
		windowEndTime = alignedNow
		windowStartTime = windowEndTime.Add(-s.Spec.Interval.Duration)
	} else {
		// Subsequent executions: start from where the last successful execution ended, minus delay, aligned to interval boundary
		start := lastSuccessfulEndTime.Add(-delay)
		windowStartTime = start.Truncate(s.Spec.Interval.Duration)
		windowEndTime = windowStartTime.Add(s.Spec.Interval.Duration)

		// Ensure we don't execute future windows
		windowEndTime = s.capWindowEndToCurrentTime(clk, windowEndTime, delay)
	}
	return
}

func (s *SummaryRule) BackfillAsyncOperations(clk clock.Clock) {
	clk = s.ensureClockIsSet(clk)

	// Get the last execution time as our starting point
	lastExecutionTime := s.GetLastExecutionTime()
	if lastExecutionTime == nil || lastExecutionTime.IsZero() {
		// No action if there's no last execution time
		return
	}

	// Get existing async operations to check for duplicates
	existingOps := s.GetAsyncOperations()

	// Create a map for quick duplicate checking based on time windows
	existingWindows := make(map[string]bool)
	for _, op := range existingOps {
		if op.StartTime != "" && op.EndTime != "" {
			key := op.StartTime + ":" + op.EndTime
			existingWindows[key] = true
		}
	}

	var newOperations []AsyncOperation

	// Calculate ingestion delay
	var delay time.Duration
	if s.Spec.IngestionDelay != nil {
		delay = s.Spec.IngestionDelay.Duration
	}

	// Calculate the current effective time (accounting for ingestion delay)
	now := clk.Now().UTC().Add(-delay)

	// Start from the last execution time and generate windows forward
	currentWindowStart := lastExecutionTime.UTC()
	intervalDuration := s.Spec.Interval.Duration

	// Generate operations from last execution time forward until we hit current time
	for {
		// Calculate the next window
		windowStart := currentWindowStart
		windowEnd := windowStart.Add(intervalDuration)

		// Stop if this window would end at or after current time
		// (only backfill completely finished intervals)
		if windowEnd.After(now) || windowEnd.Equal(now) {
			break
		}

		// Check if this time window already exists
		windowKey := windowStart.Format(time.RFC3339Nano) + ":" + windowEnd.Format(time.RFC3339Nano)
		if !existingWindows[windowKey] {
			// Create new async operation (without OperationId - backlog operation)
			newOp := AsyncOperation{
				OperationId: "", // Empty for backlog operations
				StartTime:   windowStart.Format(time.RFC3339Nano),
				EndTime:     windowEnd.Format(time.RFC3339Nano),
			}
			newOperations = append(newOperations, newOp)
			existingWindows[windowKey] = true
		}

		// Move to next interval
		currentWindowStart = windowEnd
	}

	// Add new operations to existing ones, oldest first (to ensure newest are preserved when pruning)
	allOperations := append(existingOps, newOperations...)

	// If we exceed 200 operations, prune oldest (prioritize newest windows)
	if len(allOperations) > 200 {
		// Sort by StartTime to find chronologically oldest
		sort.Slice(allOperations, func(i, j int) bool {
			timeI, errI := time.Parse(time.RFC3339Nano, allOperations[i].StartTime)
			timeJ, errJ := time.Parse(time.RFC3339Nano, allOperations[j].StartTime)

			// Handle parsing errors - fall back to array position
			if errI != nil || errJ != nil {
				return i < j
			}

			return timeI.Before(timeJ)
		})

		// Keep only the newest 200 operations
		allOperations = allOperations[len(allOperations)-200:]
	}

	// Marshal and store the updated operations
	operationsJSON, err := json.Marshal(allOperations)
	if err != nil {
		// If we can't marshal the JSON, something has gone horribly wrong
		return
	}

	// Update the condition
	condition := meta.FindStatusCondition(s.Status.Conditions, SummaryRuleOperationIdOwner)
	if condition == nil {
		condition = &metav1.Condition{}
	}

	message := string(operationsJSON)
	if condition.Message == message {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "NoBacklog"
	} else {
		condition.Status = metav1.ConditionUnknown
		condition.Reason = "InProgress"
	}
	condition.Message = message
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = s.GetGeneration()
	condition.Type = SummaryRuleOperationIdOwner

	meta.SetStatusCondition(&s.Status.Conditions, *condition)
}

// HasActiveBackfill returns true if the rule has an active backfill configuration
func (s *SummaryRule) HasActiveBackfill() bool {
	return s.Spec.Backfill != nil && s.Spec.Backfill.Start != "" && s.Spec.Backfill.End != ""
}

// IsBackfillComplete returns true if the backfill has reached or passed the end time
func (s *SummaryRule) IsBackfillComplete() bool {
	if !s.HasActiveBackfill() {
		return true // No backfill means it's "complete"
	}

	startTime, err := time.Parse(time.RFC3339, s.Spec.Backfill.Start)
	if err != nil {
		return false // Invalid start time means not complete
	}

	endTime, err := time.Parse(time.RFC3339, s.Spec.Backfill.End)
	if err != nil {
		return false // Invalid end time means not complete
	}

	return startTime.After(endTime) || startTime.Equal(endTime)
}

// GetNextBackfillWindow calculates the next time window for backfill execution
// Returns start time, end time, and whether a valid window exists
// Uses the same interval alignment as normal execution
func (s *SummaryRule) GetNextBackfillWindow(clk clock.Clock) (time.Time, time.Time, bool) {
	clk = s.ensureClockIsSet(clk)

	if !s.HasActiveBackfill() || s.IsBackfillComplete() {
		return time.Time{}, time.Time{}, false
	}

	// Parse the current backfill start position
	currentStart, err := time.Parse(time.RFC3339, s.Spec.Backfill.Start)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}

	// Parse the backfill end time
	backfillEnd, err := time.Parse(time.RFC3339, s.Spec.Backfill.End)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}

	// Use shared window calculation logic with backfill end as boundary
	windowStartTime, windowEndTime := s.calculateTimeWindow(clk, currentStart, &backfillEnd)

	// Validate that we have a meaningful window
	if windowStartTime.After(windowEndTime) || windowStartTime.Equal(windowEndTime) {
		return time.Time{}, time.Time{}, false
	}

	return windowStartTime, windowEndTime, true
}

// AdvanceBackfillProgress moves the backfill start position forward after successful execution
func (s *SummaryRule) AdvanceBackfillProgress(endTime time.Time) {
	if !s.HasActiveBackfill() {
		return
	}

	// The endTime passed in is already delay-adjusted from calculateTimeWindow,
	// so we need to reverse the delay to get the actual interval boundary position
	var delay time.Duration
	if s.Spec.IngestionDelay != nil {
		delay = s.Spec.IngestionDelay.Duration
	}

	// Add delay back to get the clean interval boundary that should be our next start position
	progressPosition := endTime.Add(delay)
	s.Spec.Backfill.Start = progressPosition.UTC().Format(time.RFC3339)
}

// ClearBackfillOperation clears the current operation ID from the backfill spec
func (s *SummaryRule) ClearBackfillOperation() {
	if s.HasActiveBackfill() {
		s.Spec.Backfill.OperationId = ""
	}
}

// SetBackfillOperation sets the operation ID for the current backfill window
func (s *SummaryRule) SetBackfillOperation(operationId string) {
	if s.HasActiveBackfill() {
		s.Spec.Backfill.OperationId = operationId
	}
}

// GetBackfillOperation returns the current backfill operation ID
func (s *SummaryRule) GetBackfillOperation() string {
	if s.HasActiveBackfill() {
		return s.Spec.Backfill.OperationId
	}
	return ""
}

// RemoveBackfill removes the backfill configuration when complete
func (s *SummaryRule) RemoveBackfill() {
	s.Spec.Backfill = nil
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
