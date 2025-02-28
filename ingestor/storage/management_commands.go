package storage

import (
	"context"
	"errors"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/scheduler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ManagementCommandConditionOwner = "managementcommand.adx-mon.azure.com"

type ManagementCommands interface {
	UpdateStatus(ctx context.Context, cmd *adxmonv1.ManagementCommand, err error) error
	List(ctx context.Context) ([]*adxmonv1.ManagementCommand, error)
}

type managementCommands struct {
	Client  client.Client
	Elector scheduler.Elector
}

func NewManagementCommands(client client.Client, elector scheduler.Elector) *managementCommands {
	return &managementCommands{
		Client:  client,
		Elector: elector,
	}
}

func (m *managementCommands) List(ctx context.Context) ([]*adxmonv1.ManagementCommand, error) {
	if m.Elector != nil && !m.Elector.IsLeader() {
		return nil, nil
	}

	if m.Client == nil {
		return nil, errors.New("no client provided")
	}

	mcs := &adxmonv1.ManagementCommandList{}
	if err := m.Client.List(ctx, mcs); err != nil {
		logger.Error("Failed to list ManagementCommands: %v", err)
		return nil, err
	}

	var result []*adxmonv1.ManagementCommand
	for i := range mcs.Items {
		if shouldSkip(mcs.Items[i]) {
			continue
		}
		result = append(result, &mcs.Items[i])
	}
	return result, nil
}

func (m *managementCommands) UpdateStatus(ctx context.Context, mc *adxmonv1.ManagementCommand, errStatus error) error {
	if m.Client == nil {
		return errors.New("no client provided")
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
		Type:               ManagementCommandConditionOwner,
		Status:             status,
		ObservedGeneration: mc.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Message:            message,
	}

	// Update or append the condition
	updateCondition(mc, condition)

	// Log the status update
	logger.Infof("Updating status for ManagementCommand %s: %s", mc.Name, message)

	// Update the status in the Kubernetes API
	if err := m.Client.Status().Update(ctx, mc); err != nil {
		logger.Errorf("Failed to update status for ManagementCommand %s: %v", mc.Name, err)
		return err
	}

	return nil
}

// shouldSkip checks if a ManagementCommand should be skipped based on its conditions e
func shouldSkip(mc adxmonv1.ManagementCommand) bool {
	for _, c := range mc.Status.Conditions {
		if c.Type == ManagementCommandConditionOwner {
			if c.Status == metav1.ConditionTrue && c.ObservedGeneration == mc.GetGeneration() {
				return true // already processed
			}
			return false // continue to drive towards goal-state
		}
	}
	return false // no status has yet been set
}

// updateCondition updates or appends the condition in the ManagementCommand status
func updateCondition(mc *adxmonv1.ManagementCommand, condition metav1.Condition) {
	var match bool
	for idx, c := range mc.Status.Conditions {
		if c.Type == ManagementCommandConditionOwner {
			condition.Reason = "Updated"
			mc.Status.Conditions[idx] = condition
			match = true
			break
		}
	}
	if !match {
		condition.Reason = "Added"
		mc.Status.Conditions = append(mc.Status.Conditions, condition)
	}
}
