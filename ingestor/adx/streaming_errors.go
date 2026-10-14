// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// streamingFailure classifies a failed streaming ingestion request.
type streamingFailure int

const (
	// streamingRetry is a transient failure.  The batch is retried with streaming ingestion.
	streamingRetry streamingFailure = iota
	// streamingThrottled means Kusto throttled the request.  The batch is retried and the streaming concurrency
	// limit is reduced.
	streamingThrottled
	// streamingTooLarge means the request exceeded the streaming size limit.  The batch uses queued ingestion.
	streamingTooLarge
	// streamingUnavailable means streaming ingestion is not available for the table, such as when the policy is
	// disabled, the schema has not propagated or the database is under maintenance.  The batch uses queued ingestion
	// and the table uses queued ingestion for a cooldown period.
	streamingUnavailable
	// streamingPermanent is a non-retryable failure, such as invalid data.  The batch uses queued ingestion.
	streamingPermanent
)

func (f streamingFailure) String() string {
	switch f {
	case streamingRetry:
		return "retry"
	case streamingThrottled:
		return "throttled"
	case streamingTooLarge:
		return "too_large"
	case streamingUnavailable:
		return "unavailable"
	case streamingPermanent:
		return "permanent"
	default:
		return "unknown"
	}
}

// kustoStatusPattern matches the HTTP status in the message of a Kusto endpoint error.  The SDK formats streaming
// errors into a string so the status code is only available in the message.
var kustoStatusPattern = regexp.MustCompile(`(?s)error from Kusto endpoint.*?\((\d{3})[^)]*\):`)

var (
	throttledCodes   = []string{"General_ThrottledIngestion"}
	tooLargeCodes    = []string{"Stream_InputStreamTooLarge", "BadRequest_FileTooLarge"}
	unavailableCodes = []string{
		"ServiceUnavailable_StreamingIngestion",
		"BadRequest_TableNotExist",
		"BadRequest_DatabaseNotExist",
		"BadRequest_EntityNotFound",
		"BadRequest_MappingReferenceWasNotFound",
		"BadRequest_TableAccessDenied",
		"BadRequest_DatabaseAccessDenied",
	}
)

// classifyStreamingError classifies an error returned by a streaming ingestion request.
func classifyStreamingError(err error) streamingFailure {
	if err == nil {
		return streamingRetry
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return streamingRetry
	}

	msg := err.Error()
	switch {
	case containsAny(msg, throttledCodes):
		return streamingThrottled
	case containsAny(msg, tooLargeCodes):
		return streamingTooLarge
	case containsAny(msg, unavailableCodes):
		return streamingUnavailable
	}

	// The SDK's message always mentions streaming so only the response body is inspected for streaming errors.
	status, body := kustoResponse(msg)
	streaming := strings.Contains(strings.ToLower(body), "streaming")
	switch {
	case status == 0:
		// No HTTP response, such as a network failure or timeout.
		return streamingRetry
	case status == 429:
		return streamingThrottled
	case status == 413:
		return streamingTooLarge
	case status == 403 || status == 404:
		return streamingUnavailable
	case status >= 400 && status < 500:
		if streaming {
			return streamingUnavailable
		}
		return streamingPermanent
	case status == 503 && streaming:
		return streamingUnavailable
	default:
		return streamingRetry
	}
}

// kustoResponse returns the HTTP status code and response body from a Kusto endpoint error message.  The status is 0
// if the message is not from a Kusto endpoint response.
func kustoResponse(msg string) (status int, body string) {
	m := kustoStatusPattern.FindStringSubmatchIndex(msg)
	if m == nil {
		return 0, ""
	}
	status, err := strconv.Atoi(msg[m[2]:m[3]])
	if err != nil {
		return 0, ""
	}
	return status, msg[m[1]:]
}

func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
