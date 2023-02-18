package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/metrics"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	kustovalues "github.com/Azure/azure-kusto-go/kusto/data/value"
)

type Executor struct {
	AlertCli interface {
		Create(ctx context.Context, endpoint string, alert alert.Alert) error
	}
	AlertAddr   string
	KustoClient Client
	ctx         context.Context
	wg          sync.WaitGroup
	closeFn     context.CancelFunc
}

func (e *Executor) Open(ctx context.Context) error {
	e.ctx, e.closeFn = context.WithCancel(ctx)
	logger.Info("Begin executing %d queries", len(rules.List()))
	for _, r := range rules.List() {
		go e.queryWorker(*r)
	}
	return nil
}

func (e *Executor) queryWorker(rule rules.Rule) {
	e.wg.Add(1)
	defer e.wg.Done()

	logger.Info("Creating query executor for %s/%s in %s executing every %s",
		rule.Namespace, rule.Name, rule.Database, rule.Interval.String())

	// do-while
	if err := e.KustoClient.Query(e.ctx, rule, e.ICMHandler); err != nil {
		logger.Error("Failed to execute query=%s/%s: %s", rule.Namespace, rule.Name, err)
		metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
	}
	ticker := time.NewTicker(rule.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			// Try to acquire a worker slot
			queue.Workers <- struct{}{}

			start := time.Now()
			logger.Info("Executing %s/%s", rule.Namespace, rule.Name)
			if err := e.KustoClient.Query(e.ctx, rule, e.ICMHandler); err != nil {
				logger.Error("Failed to execute query=%s.%s: %s", rule.Namespace, rule.Name, err)
				metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
			} else {
				metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(1)
				logger.Info("Completed %s/%s in %s", rule.Namespace, rule.Name, time.Since(start))
			}

			// Release the worker slot
			<-queue.Workers
		}
	}
}

func (e *Executor) Close() error {
	e.closeFn()
	e.wg.Wait()
	return nil
}

// ICMHandler converts rows of a query to ICMs.
func (e *Executor) ICMHandler(endpoint string, rule rules.Rule, row *table.Row) error {
	res := Notification{
		Severity:     math.MinInt64,
		CustomFields: map[string]string{},
	}

	query := rule.Query

	columns := row.ColumnNames()
	for i, value := range row.Values {
		switch strings.ToLower(columns[i]) {
		case "title":
			res.Title = value.String()
		case "description":
			res.Description = value.String()
		case "severity":
			v, err := e.asInt64(value)
			if err != nil {
				metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
				return err
			}
			res.Severity = v
		case "recipient":
			res.Recipient = value.String()
		case "summary":
			res.Summary = value.String()
		case "correlationid":
			res.CorrelationID = value.String()
		default:
			res.CustomFields[columns[i]] = value.String()
		}
	}

	if err := res.Validate(); err != nil {
		metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
		return err
	}

	for k, v := range rule.Parameters {
		switch vv := v.(type) {
		case string:
			query = strings.Replace(query, k, fmt.Sprintf("\"%s\"", vv), -1)
		default:
			metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
			return fmt.Errorf("unimplemented query type: %v", vv)
		}
	}

	url, err := kustoDeepLink(query)
	if err != nil {
		metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
		return fmt.Errorf("failed to create kusto deep link: %w", err)
	}

	if !strings.HasSuffix(endpoint, "/") {
		endpoint = endpoint + "/"
	}

	// Setup the Kusto query deep links
	link := "Execute in "
	link += fmt.Sprintf(`<a href="%s%s?query=%s">[Web]</a> `, endpoint, rule.Database, url)
	link += fmt.Sprintf(`<a href="%s%s?query=%s&web=0">[Desktop]</a>`, endpoint, rule.Database, url)

	summary := fmt.Sprintf("%s<br/><br/>%s</br><pre>%s</pre>", res.Summary, link, query)
	summary = strings.TrimSpace(summary)

	if res.CorrelationID != "" && !strings.HasPrefix(res.CorrelationID, fmt.Sprintf("%s/%s://", rule.Namespace, rule.Name)) {
		res.CorrelationID = fmt.Sprintf("%s/%s://%s", rule.Namespace, rule.Name, res.CorrelationID)
	}

	a := alert.Alert{
		Destination:   res.Recipient,
		Title:         res.Title,
		Summary:       summary,
		Description:   res.Description,
		Severity:      int(res.Severity),
		Source:        fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
		CorrelationID: res.CorrelationID,
		CustomFields:  res.CustomFields,
	}

	addr := fmt.Sprintf("%s/alerts", e.AlertAddr)
	logger.Info("Sending alert %s %v", addr, a)

	if err := e.AlertCli.Create(context.Background(), addr, a); err != nil {
		fmt.Printf("Failed to create Notification: %s\n", err)
		metrics.NotificationHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
		return nil
	}
	metrics.NotificationHealth.WithLabelValues(rule.Namespace, rule.Name).Set(1)

	//log.Infof("Created Notification %s - %s", resp.IncidentId, res.Title)

	return nil
}

func (e *Executor) asInt64(value kustovalues.Kusto) (int64, error) {
	switch t := value.(type) {
	case kustovalues.Long:
		return t.Value, nil
	case kustovalues.Real:
		return int64(t.Value), nil
	case kustovalues.String:
		v, err := strconv.ParseInt(t.Value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to convert severity to int: %w", err)
		}
		return v, nil
	case kustovalues.Int:
		return int64(t.Value), nil
	case kustovalues.Decimal:
		v, err := strconv.ParseFloat(t.Value, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to convert severity to int: %w", err)
		}
		return int64(v), nil
	default:
		return 0, fmt.Errorf("failed to convert severity to int: %s", value.String())
	}
}

func kustoDeepLink(q string) (string, error) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(q)); err != nil {
		return "", err
	}

	if err := w.Flush(); err != nil {
		return "", err
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b.Bytes()), nil
}
