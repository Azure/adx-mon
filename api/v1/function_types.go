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
	"fmt"
	"strings"
	"text/template"

	"github.com/Azure/azure-kusto-go/kusto/kql"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FunctionSpec defines the desired state of Function
type FunctionSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// To protect against specifying arbitrary KQL, we'll only allow
	// specification of the Function's body, we'll generate the scaffold
	// for `.create-or-alter function` statement

	// Database is the name of the database in which the function will be created
	Database string `json:"database"`
	// Table is the name of the table in which the function will be created. We must
	// specify a table if the function is a view, otherwise the Table name is optional.
	Table string `json:"table,omitempty"`
	// Name is the name of the function
	Name string `json:"name"`
	// IsView is a flag indicating whether the function is a view
	IsView bool `json:"isView,omitempty"`
	// Folder is the folder in which the function will be created
	Folder string `json:"folder,omitempty"`
	// DocString is the documentation string for the function
	DocString string `json:"docString,omitempty"`
	// Body is the body of the function
	Body string `json:"body"`
	// Parameters is a list of parameters for the function
	Parameters []FunctionParameter `json:"parameters,omitempty"`
}

func (fs FunctionSpec) MarshalToKQL() (*kql.Builder, error) {
	tmpl := template.Must(template.New("fn").Funcs(template.FuncMap{
		"serialize": func(parameters []FunctionParameter) string {
			var params []string
			for _, param := range fs.Parameters {
				params = append(params, param.String())
			}
			return strings.Join(params, ", ")
		},
	}).Parse(`.create-or-alter function
	with( view={{ .IsView }}, folder='{{ .Folder }}', docstring='{{ .DocString }}')
	{{ .Name }} ( {{ serialize .Parameters }} ) {
		{{ .Body }}
}`))

	var s strings.Builder
	if err := tmpl.Execute(&s, fs); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	return kql.New("").AddUnsafe(s.String()), nil
}

type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type FunctionParameter struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Fields  []Field `json:"fields,omitempty"`  // Nested fields for record type
	Default string  `json:"default,omitempty"` // Default value for timespan type
}

func (fp FunctionParameter) String() string {
	var sb strings.Builder

	sb.WriteString(fp.Name)
	sb.WriteString(": ")

	if len(fp.Fields) > 0 {
		sb.WriteString("(")
		for i, field := range fp.Fields {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s:%s", field.Name, field.Type))
		}
		sb.WriteString(")")
	} else {
		sb.WriteString(fp.Type)
		if fp.Default != "" {
			sb.WriteString(fmt.Sprintf("=%s", fp.Default))
		}
	}

	return sb.String()
}

// FunctionStatusEnum defines the possible status values for FunctionStatus
type FunctionStatusEnum string

const (
	PermanentFailure FunctionStatusEnum = "PermanentFailure"
	Failed           FunctionStatusEnum = "Failed"
	Success          FunctionStatusEnum = "Success"
)

// FunctionStatus defines the observed state of Function
type FunctionStatus struct {
	// LastTimeReconciled is the last time the Function was reconciled
	LastTimeReconciled metav1.Time `json:"lastTimeReconciled"`
	// Message is a human-readable message indicating details about the Function
	Message string `json:"message,omitempty"`
	// Error is a string that communicates any error message if one exists
	Error string `json:"error,omitempty"`
	// Status is an enum that represents the status of the Function
	Status FunctionStatusEnum `json:"status"`
}

func (fs FunctionStatus) String() string {
	return fmt.Sprintf("LastTimeReconciled: %s, Message: %s, Error: %s, Status: %s",
		fs.LastTimeReconciled, fs.Message, fs.Error, fs.Status)
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Function defines a KQL function to be maintained in the Kusto cluster
type Function struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FunctionSpec   `json:"spec,omitempty"`
	Status FunctionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FunctionList contains a list of Function
type FunctionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Function `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Function{}, &FunctionList{})
}
