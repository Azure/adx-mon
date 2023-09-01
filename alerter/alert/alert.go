package alert

type Alert struct {
	// Destination is the identifier indicating where the alert should be routed by the alert receiver.
	Destination string

	// Title is the title of the alert.
	Title string

	// Summary is a short summary of the alert.
	Summary string

	// Description is a longer details of the alert.
	Description string

	// Severity is the severity of the alert.
	Severity int

	// Source is the identifier of the source of the alert.  This is typically the name of the alert rule.
	Source string

	// Correlation ID is an identifier for an alert or deduplicate multiple events.  This can be used by receivers
	// to indicate that an alert is still firing.
	CorrelationID string

	// CustomerFields is a map of key/value pairs that provide additional information about the alert.
	CustomFields map[string]string
}
