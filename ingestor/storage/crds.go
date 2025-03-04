package storage

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/scheduler"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDHandler interface {
	List(ctx context.Context, list client.ObjectList) error
	UpdateStatus(ctx context.Context, obj client.Object, errStatus error) error
}

type crdHandler struct {
	Client  client.Client
	Elector scheduler.Elector
}

func NewCRDHandler(client client.Client, elector scheduler.Elector) CRDHandler {
	return &crdHandler{
		Client:  client,
		Elector: elector,
	}
}

func (c *crdHandler) List(ctx context.Context, list client.ObjectList) error {
	if c.Elector != nil && !c.Elector.IsLeader() {
		return nil
	}

	if c.Client == nil {
		return errors.New("no client provided")
	}

	if err := c.Client.List(ctx, list); err != nil {
		logger.Error("Failed to list CRDs: %v", err)
		return err
	}

	var filtered []runtime.Object
	meta.EachListItem(list, func(item runtime.Object) error {
		obj, ok := item.(client.Object)
		if !ok {
			return nil
		}
		statusObj, ok := obj.(adxmonv1.ConditionedObject)
		if !ok {
			return nil
		}
		condition := statusObj.GetCondition()
		if condition != nil && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == obj.GetGeneration() {
			return nil
		}
		filtered = append(filtered, obj)
		return nil
	})

	meta.SetList(list, filtered)
	return nil
}

func (c *crdHandler) UpdateStatus(ctx context.Context, obj client.Object, errStatus error) error {
	if c.Client == nil {
		return errors.New("no client provided")
	}

	statusObj, ok := obj.(adxmonv1.ConditionedObject)
	if !ok {
		return errors.New("object does not implement ConditionedObject")
	}

	var (
		status  = metav1.ConditionTrue
		message = ""
	)
	if errStatus != nil {
		status = metav1.ConditionFalse
		message = errStatus.Error()
	}

	condition := metav1.Condition{
		Status:             status,
		ObservedGeneration: obj.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Message:            message,
	}

	statusObj.SetCondition(condition)
	logger.Infof("Updating status for %s/%s: %s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), message)

	if err := c.Client.Status().Update(ctx, obj); err != nil {
		logger.Errorf("Failed to update status for %s/%s: %v", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
		return err
	}

	return nil
}

func ConvertToTypedList(objects []client.Object, list client.ObjectList) error {
	// Get the value of the list object
	listValue := reflect.ValueOf(list).Elem()

	// Find the Items field
	itemsField := listValue.FieldByName("Items")
	if !itemsField.IsValid() {
		return fmt.Errorf("list object doesn't have Items field")
	}

	// Get the type of the slice elements
	itemsType := itemsField.Type().Elem()

	// Create a new slice to hold the converted items
	newSlice := reflect.MakeSlice(reflect.SliceOf(itemsType), 0, len(objects))

	// Convert each object to the right type
	for _, obj := range objects {
		// Check if the object can be converted to the target type
		objValue := reflect.ValueOf(obj)
		if !objValue.Type().ConvertibleTo(itemsType) {
			// Try pointer/value conversion
			if objValue.Type().Elem().ConvertibleTo(itemsType) {
				// We have *Type but need Type
				newSlice = reflect.Append(newSlice, objValue.Elem())
			} else if objValue.Type().ConvertibleTo(reflect.PointerTo(itemsType)) {
				// We have Type but need *Type
				newSlice = reflect.Append(newSlice, objValue.Addr())
			} else {
				return fmt.Errorf("cannot convert %T to slice element type %s", obj, itemsType)
			}
		} else {
			newSlice = reflect.Append(newSlice, objValue)
		}
	}

	// Set the Items field
	itemsField.Set(newSlice)
	return nil
}
