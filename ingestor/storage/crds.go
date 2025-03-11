package storage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/google/uuid"
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
	Client       client.Client
	ControllerId string
}

func NewCRDHandler(client client.Client) CRDHandler {
	return &crdHandler{
		Client:       client,
		ControllerId: "controller-" + uuid.NewString(),
	}
}

func (c *crdHandler) List(ctx context.Context, list client.ObjectList, filters ...ListFilterFunc) error {
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

		// Apply any filters before checking ownership. We want to ignore ownership
		// if we're otherwise not interested in this instance. For example, if this
		// instance has already been reconciled and is otherwise up-to-date, no need
		// to try and establish ownership. This will prevent us from thrashing by
		// constantly updating the resource last-updated annotation.
		for _, filter := range filters {
			if filter(obj) {
				return nil
			}
		}

		if !CheckOwnership(ctx, obj, c.Client, c.ControllerId) {
			if logger.IsDebug() {
				logger.Debugf("Skipping %s/%s, not owned by this controller", obj.GetNamespace(), obj.GetName())
			}
			return nil
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

// CheckOwnership returns true if the object is owned by the controller or if the contoller has successfully
// claimed ownership of the resource.
func CheckOwnership(ctx context.Context, obj client.Object, ctrlClient client.Client, ownerId string) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	owner, owned := annotations[adxmonv1.ControllerOwnerKey]
	lastUpdated, hasLastUpdated := annotations[adxmonv1.LastUpdatedKey]

	// If unowned, claim it
	if !owned {
		if logger.IsDebug() {
			logger.Debugf("Claiming unowned resource %s/%s", obj.GetNamespace(), obj.GetName())
		}
		annotations[adxmonv1.ControllerOwnerKey] = ownerId
		annotations[adxmonv1.LastUpdatedKey] = metav1.Now().Format(time.RFC3339)
		obj.SetAnnotations(annotations)
		if err := ctrlClient.Update(ctx, obj); err == nil {
			if logger.IsDebug() {
				logger.Debugf("Claimed ownership of %s/%s: %v", obj.GetNamespace(), obj.GetName(), err)
			}
			owner = ownerId
		}
	}

	// If owned by someone else, skip unless stale
	if owner != ownerId {
		if hasLastUpdated {
			lastTime, err := time.Parse(time.RFC3339, lastUpdated)
			if err == nil && time.Since(lastTime) > 5*time.Minute { // Stale after 5 minutes
				if logger.IsDebug() {
					logger.Debugf("Taking over stale resource %s/%s from %s", obj.GetNamespace(), obj.GetName(), owner)
				}
				annotations[adxmonv1.ControllerOwnerKey] = ownerId
				annotations[adxmonv1.LastUpdatedKey] = metav1.Now().Format(time.RFC3339)
				obj.SetAnnotations(annotations)
				if err := ctrlClient.Update(ctx, obj); err == nil {
					if logger.IsDebug() {
						logger.Debugf("Claimed ownership of %s/%s: %v", obj.GetNamespace(), obj.GetName(), err)
					}
				}
			} else {
				return false // Skip, not stale
			}
		} else {
			return false // Skip, owned but no timestamp
		}
	}

	// We own this instance, set the last-updated annotation to keep ownership active
	annotations[adxmonv1.LastUpdatedKey] = metav1.Now().Format(time.RFC3339)
	obj.SetAnnotations(annotations)

	return true
}
