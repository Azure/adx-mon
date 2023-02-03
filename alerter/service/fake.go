package service

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"io"
	"net/http"
)

type fakeKustoClient struct {
	endpoint string
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
	flag.String("test.v", "", "")
	if err := flag.CommandLine.Set("test.v", "true"); err != nil {
		panic(err)
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
			logger.Error("failed to read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		a := alert.Alert{}
		if err := json.Unmarshal(b, &a); err != nil {
			logger.Error("failed to unmarshal request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		logger.Info("Fake Alert Notification Recieved: %v", a)
		w.WriteHeader(http.StatusCreated)
	})
}
