package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/Azure/adx-mon/alert"

	"github.com/Azure/adx-mon/logger"
	"github.com/davecgh/go-spew/spew"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type Executor struct {
	AlertCli  *alert.Client
	AlertAddr string
}

func (e *Executor) Execute(ctx context.Context, client Client, log logger.Logger) error {
	logger.Info("Begin executing %d queries", len(rules.List()))
	var wg sync.WaitGroup
	for _, r := range rules.List() {
		wg.Add(1)
		go func(rule rules.Rule) {
			log.Info("Creating query executor for %s in %s", rule.DisplayName, rule.Database)
			// do-while
			handler := e.ICMHandler
			if isMetric(rule) {
				handler = e.MetricHandler
			}
			if err := client.Query(ctx, rule, handler); err != nil {
				log.Error("Failed to execute query=%s: %w", rule.DisplayName, err)
			}
			ticker := time.NewTicker(rule.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					wg.Done()
					return
				case <-ticker.C:
					queue.Workers <- struct{}{}
					start := time.Now()
					log.Info("Executing %s", rule.DisplayName)
					if err := client.Query(ctx, rule, handler); err != nil {
						log.Error("Failed to execute query=%s: %w", rule.DisplayName, err)
					}
					log.Info("Completed %s in %s", rule.DisplayName, time.Since(start))
					<-queue.Workers
				}
			}

		}(*r)
	}
	wg.Wait()
	return ctx.Err()
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
	logger.Debug("Incrementing metric: name=%s by=%d with=%v", rule.DisplayName, metric, dimensions)

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
		return fmt.Errorf("failed to create Notification: no routing id for %s", rule.DisplayName)
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

	if res.CorrelationID != "" && !strings.HasPrefix(res.CorrelationID, rule.DisplayName+"://") {
		res.CorrelationID = fmt.Sprintf("%s://%s", rule.DisplayName, res.CorrelationID)
	}

	a := alert.Alert{
		Destination:   rule.RoutingID,
		Title:         res.Title,
		Summary:       summary,
		Description:   res.Description,
		Severity:      int(res.Severity),
		Source:        rule.DisplayName,
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
	logger.Info("Sending alert %s %s", addr, spew.Sdump(a))
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

func isMetric(r rules.Rule) bool {
	return len(r.Dimensions) != 0 && r.Value != ""
}
