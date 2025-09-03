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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AlertRuleSpec defines the desired state of AlertRule
type AlertRuleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Database string `json:"database,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == '' || duration(self) > duration('0s')",message="interval must be a valid positive duration"
	Interval metav1.Duration `json:"interval,omitempty"`
	Query    string          `json:"query,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == '' || duration(self) >= duration('0s')",message="autoMitigateAfter must be a valid duration"
	AutoMitigateAfter metav1.Duration `json:"autoMitigateAfter,omitempty"`
	Destination       string          `json:"destination,omitempty"`

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
	rule := make(map[string]interface{})
	if err := json.Unmarshal(data, &rule); err != nil {
		return err
	}
	a.Database = rule["database"].(string)
	if rule["interval"] != nil {
		dur := rule["interval"].(string)
		if dur != "" {
			d, err := time.ParseDuration(dur)
			if err != nil {
				return err
			}
			a.Interval = metav1.Duration{Duration: d}
		}
	}

	a.Query = rule["query"].(string)

	if rule["autoMitigateAfter"] != nil {
		mit := rule["autoMitigateAfter"].(string)
		if mit != "" {
			d, err := time.ParseDuration(mit)
			if err != nil {
				return err
			}
			a.AutoMitigateAfter = metav1.Duration{Duration: d}
		}
	}
	a.Destination = rule["destination"].(string)

	// The type for Criteria has changed to map[string][]string.  This handles backwards compatibility where
	// some existing alerts may have a string value instead of a list of strings.
	criteria := rule["criteria"]
	if s, ok := criteria.(map[string]interface{}); ok && s != nil {
		a.Criteria = make(map[string][]string)
		for k, raw := range s {
			switch val := raw.(type) {
			case string:
				a.Criteria[k] = []string{val}
			case []interface{}:
				for _, v := range val {
					if str, ok := v.(string); ok {
						a.Criteria[k] = append(a.Criteria[k], str)
					}
				}
			}
		}
	}

	// Optional criteriaExpression (string). If absent or wrong type, leave zero value.
	if expr, ok := rule["criteriaExpression"].(string); ok {
		a.CriteriaExpression = expr
	}

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
