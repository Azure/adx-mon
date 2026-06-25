package alerter

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"strings"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/pkg/logger"
)

const icmTitleMaxLength = 150

type fakeKustoClient struct {
	endpoint string
}

func (f fakeKustoClient) Endpoint() string {
	return f.endpoint
}

func fakeAlertHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Errorf("Failed to read request body: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		a := alert.Alert{}
		if err := json.Unmarshal(b, &a); err != nil {
			logger.Errorf("Failed to unmarshal request body: %s", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		logger.Infof("Fake Alert Notification Recieved: %v", a)
		w.WriteHeader(http.StatusCreated)
	})
}

type lintAlertHandler struct {
	alertCount       map[string]int
	hasFailedQueries bool
	failures         []string
}

func NewLinter() *lintAlertHandler {
	return &lintAlertHandler{
		alertCount: make(map[string]int),
	}
}

// Create implements the alert.Client interface for testing purposes.
// It logs out the type of failure and tracks that we have had failed queries.
func (lh *lintAlertHandler) Create(ctx context.Context, endpoint string, alert alert.Alert) error {
	lh.alertCount[alert.CorrelationID]++
	if len(alert.Title) > icmTitleMaxLength {
		logger.Errorf("Title Exceeded Max Length: %s", alert.Title)
		lh.hasFailedQueries = true
		lh.failures = append(lh.failures, "title exceeded max length: "+alert.Title)
	}

	if strings.HasPrefix(alert.CorrelationID, "alert-failure") {
		lh.hasFailedQueries = true
		lh.failures = append(lh.failures, lintFailureMessage(alert))
	}
	return nil
}

func (lh *lintAlertHandler) HasFailedQueries() bool {
	return lh.hasFailedQueries
}

func (lh *lintAlertHandler) Err() error {
	msg := "failed to lint rules"
	if len(lh.failures) > 0 {
		msg += ": " + strings.Join(lh.failures, "; ")
	}
	return errors.New(msg)
}

func lintFailureMessage(alert alert.Alert) string {
	if alert.Description != "" {
		return alert.Description
	}
	if alert.Summary != "" {
		return firstPreBlock(alert.Summary)
	}
	if alert.Title != "" {
		return alert.Title
	}
	return alert.CorrelationID
}

func firstPreBlock(s string) string {
	start := strings.Index(s, "<pre>")
	if start == -1 {
		return s
	}
	start += len("<pre>")

	end := strings.Index(s[start:], "</pre>")
	if end == -1 {
		return html.UnescapeString(s[start:])
	}
	return html.UnescapeString(s[start : start+end])
}

func (lh *lintAlertHandler) Log() {
	for k, v := range lh.alertCount {
		logger.Infof("Alert %s was sent %d times", k, v)
	}
}
