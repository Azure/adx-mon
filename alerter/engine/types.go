package engine

import (
	"fmt"
	"math"
)

type Notification struct {
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

	// Recipient is the destination of the Notification.  Typically, a queue or email address.
	Recipient string `kusto:"Recipient"`

	// CustomFields are any additional fields that are not part of the Notification struct.
	CustomFields map[string]string
}

func (i Notification) Validate() error {
	if len(i.Title) == 0 || len(i.Title) > 512 {
		return fmt.Errorf("title must be between 1 and 512 chars")
	}
	if i.Severity == math.MinInt64 {
		return fmt.Errorf("severity must be specified")
	}
	return nil
}
