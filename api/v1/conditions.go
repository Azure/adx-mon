package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionedObject defines interface for objects that can have conditions
// +k8s:deepcopy-gen=false
type ConditionedObject interface {
	GetCondition() *metav1.Condition
	SetCondition(condition metav1.Condition)
}
