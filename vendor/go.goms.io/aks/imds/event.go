package imds

import (
	"strconv"
	"time"
)

// ScheduledEvents is the payload received by the Azure metadata service when querying the scheduled events metadata
// service endpoint.
type ScheduledEvents struct {
	DocumentIncarnation int              `json:"DocumentIncarnation"`
	Events              []ScheduledEvent `json:"Events"`
}

type ScheduledEvent struct {
	// Description is a human friendly description of the event. There is no
	// formal description of what data will be in this field. It should not
	// be relied on for programmatic use and should be considered
	// informational only.
	Description string `json:"Description"`

	// Duration is the time duration of the event.
	DurationInSeconds int `json:"DurationInSeconds"`

	// ID is a globally unique identifier for the event. The ID is used when constructing a StartRequest as the the
	// value of the StartRequest.EventId field.
	ID string `json:"EventId"`

	// NotBefore is the earliest time the event can start. It is an upper bound on when cleanup must be finished before
	// the action indicated by the event type is executed regardless of whether the event has been acknowledged or not.
	NotBefore NotBeforeTime `json:"NotBefore"`

	// ResourceType is the type of resource the event impacts.
	ResourceType string `json:"ResourceType"`

	// Resources is a slice of resource identifiers for affected resources. This contains the ID of the impacted Azure
	// VM and should be checked before taking any action.
	Resources []string `json:"Resources"`

	// Type is the kind of event being handled.
	Type string `json:"EventType"`

	// Status is the status of the event. The case of the value is not fixed so
	// consumers should use case-insensitive comparison such as
	// strings.EqualFold or normalize to a specific case.
	Status string `json:"EventStatus"`

	// Source is the origin of the event, for example, "Platform" or "User". The
	// case of the value is not fixed so consumers should use a case-insensitive
	// comparison such as strings.EqualFold or normalize to a specific case.
	Source string `json:"EventSource"`
}

// StartRequests is the wrapper that we send back to the Azure metadata service instructing it to proceed with one or
// more events.
type StartRequests struct {
	StartRequests []StartRequest `json:"StartRequests"`
}

// StartRequest contains the Event identifier and is sent back to the Azure metadata service to indicate completion of
// any pre-event logic (e.g. shutdown).
type StartRequest struct {
	EventID string `json:"EventId"`
}

// NotBeforeTime is a wrapper around time.Time to handle a non ISO801/RFC3339 formatted time sent by the scheduled
// event service API. The time format used by the scheduled event metadata service API is RFC1123 format.
//
// Consumers should call time.IsZero() to check if the time is valid and in the future. The scheduled events
// API sometimes sends empty time data (usually seen with Started) events.
type NotBeforeTime struct {
	time.Time
}

func (nbt *NotBeforeTime) UnmarshalJSON(b []byte) (err error) {
	// NOTE <phlombar@microsoft.com> This func should _NEVER_ return an
	// error. The scheduled events API is flaky as any other Azure API
	// and sometimes sends invalid date data. If the date cannot be
	// parsed then unmarshalling fails and code that depends on
	// receiving the event will never work.
	//
	// We always return nil but leave the time value as the
	// time zero-val. Consumers can check if time.IsZero() returns
	// true.
	nbtString, err := strconv.Unquote(string(b))
	if err != nil {
		//nolint:nilerr
		return nil
	}

	nbt.Time, _ = time.Parse(time.RFC1123, nbtString)

	return nil
}

func (nbt *NotBeforeTime) MarshalJSON() ([]byte, error) {
	if nbt.Time.IsZero() {
		return []byte{}, nil
	}

	return []byte(strconv.Quote(nbt.Time.Format(time.RFC1123))), nil
}
