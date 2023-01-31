package engine

import "fmt"

type Notification struct {
	// Region maps to the Location/Region Notification field.
	Region       string `kusto:"Region"`
	UnderlayName string `kusto:"UnderlayName"`

	// Title maps to the Title Notification field.
	Title string `kusto:"Title"`

	// Summary maps to the Description Notification field.
	Summary string `kusto:"Summary"`

	// Description maps to the Description Notification field.
	Description string `kusto:"Description"`

	// Severity maps to the Severity Notification field.
	Severity int64 `kusto:"Severity"`

	// CorrelationID maps to the CorrelationId Notification field.  If a correlation ID is specified, the hit count for
	// the original Notification will be incremented on each firing of the Notification.
	CorrelationID string `kusto:"CorrelationId"`

	// Cluster maps to the Location/Cluster Notification field.  This is typically the underlay resource group in infra ICMs.
	Cluster string `kusto:"Cluster"`

	// Role maps to the Location/Role Notification field.
	Role string `kusto:"Role"`

	// Slice maps to the Location/Slice Notification field.  This is typically the underlay host name for infra ICMs.
	Slice string `kusto:"Slice"`
}

func (i Notification) Validate() error {
	if len(i.Title) == 0 || len(i.Title) > 512 {
		return fmt.Errorf("Title must be between 1 and 512 chars")
	}
	return nil
}
