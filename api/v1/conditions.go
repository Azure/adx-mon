package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ConditionedObject interface {
	GetCondition() *metav1.Condition
	SetCondition(condition metav1.Condition)
}
