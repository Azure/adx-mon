package alerter

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"strings"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
)

type fakeKustoClient struct {
	endpoint string
}

func (f fakeKustoClient) Mgmt(ctx context.Context, db string, query kusto.Stmt, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	return nil, nil
}

func (f fakeKustoClient) Query(ctx context.Context, db string, query kusto.Stmt, options ...kusto.QueryOption) (*kusto.RowIterator, error) {
	iter := &kusto.RowIterator{}

	rows, err := kusto.NewMockRows(table.Columns{
		{Name: "Title", Type: types.String},
		{Name: "Severity", Type: types.Long},
		{Name: "Summary", Type: types.String},
		{Name: "CorrelationId", Type: types.String},
	})
	if err != nil {
		return nil, err
	}
	rows.Row(value.Values{
		value.String{Value: "Fake Alert", Valid: true},
		value.Long{Value: 1, Valid: true},
		value.String{Value: "Fake Alert Summary", Valid: true},
		value.String{Value: "Fake CorrelationId", Valid: true},
	})

	// Work-around to prevent the iter.Mock call from panicing because it's not running in a test.
	if flag.Lookup("test.v") == nil {
		flag.String("test.v", "", "")
		if err := flag.CommandLine.Set("test.v", "true"); err != nil {
			panic(err)
		}
	}
	if err := iter.Mock(rows); err != nil {
		panic(err)
	}
	return iter, nil
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
}

func NewLinter() *lintAlertHandler {
	return &lintAlertHandler{
		alertCount: make(map[string]int),
	}
}

func (lh *lintAlertHandler) Handler() http.Handler {
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
		lh.alertCount[a.CorrelationID]++
		if strings.HasPrefix(a.CorrelationID, "alert-failure") {
			lh.hasFailedQueries = true
		}
		w.WriteHeader(http.StatusCreated)
	})
}

func (lh *lintAlertHandler) HasFailedQueries() bool {
	return lh.hasFailedQueries
}

func (lh *lintAlertHandler) Log() {
	for k, v := range lh.alertCount {
		logger.Infof("Alert %s was sent %d times", k, v)
	}
}
