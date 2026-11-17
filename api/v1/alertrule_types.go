/*
Copyright 2023.

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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MinAlertRuleInterval is the shortest interval an AlertRule may run at. Keep it
// in sync with the validation rule on AlertRuleSpec.Interval.
const MinAlertRuleInterval = 30 * time.Second

// MaxAlertRuleDestinations is the maximum number of entries permitted in AlertRuleSpec.Destinations.
const MaxAlertRuleDestinations = 32

// MaxAlertRuleDestinationLength is the maximum Unicode code-point length permitted per AlertRuleSpec.Destinations item,
// bounding Kubernetes CRD CEL uniqueness admission cost well below the static budget.
const MaxAlertRuleDestinationLength = 512

// AlertRuleSpec defines the desired state of AlertRule
// +kubebuilder:validation:XValidation:rule="!(has(self.destination) && size(self.destination) > 0 && has(self.destinations) && size(self.destinations) > 0)",message="destination and destinations are mutually exclusive"
type AlertRuleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Database string `json:"database,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self != '' && duration(self) >= duration('30s')",message="interval must be a duration of at least 30s"
	Interval metav1.Duration `json:"interval,omitempty"`
	Query    string          `json:"query,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == '' || duration(self) >= duration('0s')",message="autoMitigateAfter must be a valid duration"
	AutoMitigateAfter metav1.Duration `json:"autoMitigateAfter,omitempty"`
	// Destination is the legacy singular notification target identifier. Deprecated in favor of Destinations;
	// mutually exclusive with Destinations.
	Destination string `json:"destination,omitempty"`
	// Destinations is an ordered list of notification target identifiers used to route alerts across receivers.
	// Mutually exclusive with deprecated Destination.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=512
	// +kubebuilder:validation:XValidation:rule="self.all(x, self.exists_one(y, x == y))",message="destinations must not contain duplicate entries"
	Destinations []string `json:"destinations,omitempty"`

	// Key/Value pairs used to determine when an alert can execute.  If empty, always execute.  Keys and values
	// are deployment specific and configured on alerter instances.  For example, an alerter instance may be
	// started with `--tag cloud=public`. If an AlertRule has `criteria: {cloud: public}`, then the rule will only
	// execute on that alerter. Any key/values pairs must match (case-insensitive) for the rule to execute.
	Criteria map[string][]string `json:"criteria,omitempty"`

	// CriteriaExpression is an optional CEL (Common Expression Language) expression that provides a richer way
	// to determine if the alert should execute. The CEL environment is dynamically constructed from the alerter's
	// execution context (cloud, region, and any --tag key/value pairs). All variables are exposed as strings and
	// can be referenced directly by their tag name. For example:
	//
	//   criteriaExpression: "cloud == 'public' && region in ['eastus','westus'] && env == 'prod'"
	//
	// Execution semantics:
	//   * If neither criteria nor criteriaExpression are specified, the rule always executes.
	//   * If only criteriaExpression is specified, it must evaluate to true for the rule to execute.
	//   * If only criteria (map) is specified, existing matching behavior (any key/value match) applies.
	//   * If both are specified, the rule executes when BOTH the criteria map matches AND the expression evaluates to true.
	//
	// An invalid expression (parse or type check error) will treated as an error and prevent the rule from executing.
	CriteriaExpression string `json:"criteriaExpression,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface for backward compatibility with and older version of the
// Criteria field.  The older version was a map[string]string.  This function will convert the older version to the
// new version.
func (a *AlertRuleSpec) UnmarshalJSON(data []byte) error {
	var rule map[string]any
	if err := json.Unmarshal(data, &rule); err != nil {
		return err
	}
	if rule == nil {
		*a = AlertRuleSpec{}
		return nil
	}

	var out AlertRuleSpec

	if raw, ok := rule["database"]; ok && raw != nil {
		db, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("invalid type for AlertRuleSpec.database: expected string, got %T", raw)
		}
		out.Database = db
	}

	if raw, ok := rule["interval"]; ok && raw != nil {
		dur, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("invalid type for AlertRuleSpec.interval: expected string, got %T", raw)
		}
		if dur != "" {
			d, err := time.ParseDuration(dur)
			if err != nil {
				return err
			}
			out.Interval = metav1.Duration{Duration: d}
		}
	}

	if raw, ok := rule["query"]; ok && raw != nil {
		q, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("invalid type for AlertRuleSpec.query: expected string, got %T", raw)
		}
		out.Query = q
	}

	if raw, ok := rule["autoMitigateAfter"]; ok && raw != nil {
		mit, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("invalid type for AlertRuleSpec.autoMitigateAfter: expected string, got %T", raw)
		}
		if mit != "" {
			d, err := time.ParseDuration(mit)
			if err != nil {
				return err
			}
			out.AutoMitigateAfter = metav1.Duration{Duration: d}
		}
	}

	if raw, ok := rule["destination"]; ok && raw != nil {
		dest, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("invalid type for AlertRuleSpec.destination: expected string, got %T", raw)
		}
		out.Destination = dest
	}

	if raw, ok := rule["destinations"]; ok && raw != nil {
		list, isList := raw.([]any)
		if !isList {
			return fmt.Errorf("invalid type for AlertRuleSpec.destinations: expected []string, got %T", raw)
		}
		out.Destinations = make([]string, 0, len(list))
		for i, item := range list {
			str, isStr := item.(string)
			if !isStr {
				return fmt.Errorf("invalid type for AlertRuleSpec.destinations[%d]: expected string, got %T", i, item)
			}
			out.Destinations = append(out.Destinations, str)
		}
	}

	// The type for Criteria has changed to map[string][]string.  This handles backwards compatibility where
	// some existing alerts may have a string value instead of a list of strings.
	if rawCriteria, ok := rule["criteria"]; ok && rawCriteria != nil {
		s, isMap := rawCriteria.(map[string]any)
		if !isMap {
			return fmt.Errorf("invalid type for AlertRuleSpec.criteria: expected object, got %T", rawCriteria)
		}
		out.Criteria = make(map[string][]string, len(s))
		for k, raw := range s {
			switch val := raw.(type) {
			case nil:
				out.Criteria[k] = nil
			case string:
				out.Criteria[k] = []string{val}
			case []any:
				vals := make([]string, 0, len(val))
				for i, v := range val {
					str, isStr := v.(string)
					if !isStr {
						return fmt.Errorf("invalid type for AlertRuleSpec.criteria[%q][%d]: expected string, got %T", k, i, v)
					}
					vals = append(vals, str)
				}
				out.Criteria[k] = vals
			default:
				return fmt.Errorf("invalid type for AlertRuleSpec.criteria[%q]: expected string or []string, got %T", k, raw)
			}
		}
	}

	if raw, ok := rule["criteriaExpression"]; ok && raw != nil {
		expr, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("invalid type for AlertRuleSpec.criteriaExpression: expected string, got %T", raw)
		}
		out.CriteriaExpression = expr
	}

	*a = out
	return nil
}

// AlertRuleStatus defines the observed state of AlertRule
type AlertRuleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Status        string      `json:"status,omitempty"`
	Message       string      `json:"message,omitempty"`
	LastQueryTime metav1.Time `json:"lastQueryTime,omitempty"`
	LastAlertTime metav1.Time `json:"lastAlertTime,omitempty"`

	LastEvaluationDurationMilliseconds int64 `json:"lastEvaluationDurationMilliseconds,omitempty"`
	LastRowsReturned                   int64 `json:"lastRowsReturned,omitempty"`
	LastAlertsGenerated                int64 `json:"lastAlertsGenerated,omitempty"`
	// Conditions provide standardized status signaling. Controllers may
	// set shared conditions defined in conditions.go such as ConditionCriteria,
	// ConditionDestinationMatched, and ConditionNotificationDelivery.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AlertRule is the Schema for the alertrules API
type AlertRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlertRuleSpec   `json:"spec,omitempty"`
	Status AlertRuleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AlertRuleList contains a list of AlertRule
type AlertRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlertRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AlertRule{}, &AlertRuleList{})
}
