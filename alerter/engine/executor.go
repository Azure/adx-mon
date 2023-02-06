package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/logger"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type Executor struct {
	AlertCli    *alert.Client
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
			}
			logger.Info("Completed %s/%s in %s", rule.Namespace, rule.Name, time.Since(start))

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

func (e *Executor) MetricHandler(endpoint string, rule rules.Rule, row *table.Row) error {
	var (
		columns    []string
		dimensions = make(map[string]string)
		metric     int
	)
	columns = row.ColumnNames()
	for i, value := range row.Values {
		if rule.Metric != nil {
			if columnIsDimension(rule.Dimensions, value.String()) {
				dimensions[columns[i]] = value.String()
				logger.Debug("Found metric dimension: %s: %s", columns[i], dimensions[columns[i]])
				continue
			}
			if rule.Value == columns[i] {
				v := value.String()
				vv, err := strconv.Atoi(v)
				if err != nil {
					logger.Error("Metric value cannot be converted to a number: %s", v)
					return fmt.Errorf("failed to convert metric value=%s: %w", v, err)
				}
				metric = vv
				logger.Debug("Found metric value: %d", vv)
				continue
			}
		}
	}

	rule.Metric.With(dimensions).Add(float64(metric))
	logger.Debug("Incrementing metric: namespace=%s name=%s by=%d with=%v", rule.Namespace, rule.Name, metric, dimensions)

	return nil
}

// ICMHandler converts rows of a query to ICMs.
func (e *Executor) ICMHandler(endpoint string, rule rules.Rule, row *table.Row) error {
	res := Notification{}
	if err := row.ToStruct(&res); err != nil {
		return fmt.Errorf("failed to decode Notification: %w", err)
	}
	query := rule.Query

	if err := res.Validate(); err != nil {
		return err
	}

	if rule.RoutingID == "" {
		return fmt.Errorf("failed to create Notification: no routing id for %s/%s", rule.Namespace, rule.Name)
	}

	for k, v := range rule.Parameters {
		switch vv := v.(type) {
		case string:
			query = strings.Replace(query, k, fmt.Sprintf("\"%s\"", vv), -1)
		default:
			return fmt.Errorf("unimplemented query type: %v", vv)
		}
	}

	url, err := kustoDeepLink(query)
	if err != nil {
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
		Destination:   rule.RoutingID,
		Title:         res.Title,
		Summary:       summary,
		Description:   res.Description,
		Severity:      int(res.Severity),
		Source:        fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
		CorrelationID: res.CorrelationID,
		CustomFields: map[string]string{ // TODO: These are Azure specific.  Need to make this generic.
			"TSG":     rule.TSG,
			"Region":  res.Region,
			"Role":    res.Role,
			"Cluster": res.Cluster,
			"Slice":   res.Slice,
		},
	}

	addr := fmt.Sprintf("%s/alerts", e.AlertAddr)
	logger.Info("Sending alert %s %v", addr, a)
	if err := e.AlertCli.Create(context.Background(), addr, a); err != nil {
		fmt.Printf("Failed to log Notification: %s\n", err)
		return nil
	}

	//log.Infof("Created Notification %s - %s", resp.IncidentId, res.Title)

	return nil
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

func columnIsDimension(dimensions []string, column string) bool {
	for _, d := range dimensions {
		if d == column {
			return true
		}
	}
	return false
}
