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

type ListFilterFunc func(client.Object) bool

// FilterCompleted will filter out objects that are completed, as determined
// by their Conditions being in a "True" state and the ObservedGeneration
// matching the current generation of the object.
func FilterCompleted(obj client.Object) bool {
	statusObj, ok := obj.(adxmonv1.ConditionedObject)
	if !ok {
		return false
	}

	condition := statusObj.GetCondition()
	return condition != nil && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == obj.GetGeneration()
}

type CRDHandler interface {
	List(ctx context.Context, list client.ObjectList, filters ...ListFilterFunc) error
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

func (c *crdHandler) List(ctx context.Context, list client.ObjectList, filters ...ListFilterFunc) error {
	if c.Elector != nil && !c.Elector.IsLeader() {
		return nil
	}

	// TODO (jesthom) Method is invoked for each database for each task, we probably want to
	// switch to some sort of shared cache.
	// controller-runtime implements such a caching mechanism
	//
	// mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{})
	// then whenever you retrieve a client, you intherit the caching mechanism
	// client := mgr.GetClient()

	if c.Client == nil {
		return errors.New("no client provided")
	}

	if err := c.Client.List(ctx, list); err != nil {
		return fmt.Errorf("failed to list CRDs: %w", err)
	}

	var filtered []runtime.Object
	err := meta.EachListItem(list, func(item runtime.Object) error {
		obj, ok := item.(client.Object)
		if !ok {
			return nil
		}
		for _, filter := range filters {
			if filter(obj) {
				return nil
			}
		}
		filtered = append(filtered, obj)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to filter list items: %w", err)
	}

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
		Status:  status,
		Message: message,
	}

	statusObj.SetCondition(condition)
	logger.Infof("Updating status for %s/%s: %s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), message)

	if err := c.Client.Status().Update(ctx, obj); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
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
