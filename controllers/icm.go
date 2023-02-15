package controllers

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Azure/adx-mon/alert"
	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

// ICMHandler converts rows of a query to ICMs.
func (r *AlertRuleReconciler) ICMHandler(endpoint string, rule adxmonv1.AlertRule, row *table.Row) error {
	res := Notification{}
	if err := row.ToStruct(&res); err != nil {
		return fmt.Errorf("failed to decode Notification: %w", err)
	}
	query := rule.Spec.Query

	if err := res.Validate(); err != nil {
		return err
	}

	if rule.Spec.RoutingID == "" {
		return fmt.Errorf("failed to create Notification: no routing id for %s/%s", rule.Namespace, rule.Name)
	}

	for k, v := range rule.Spec.Parameters {
		query = strings.Replace(query, k, fmt.Sprintf("\"%s\"", v), -1)
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
	link += fmt.Sprintf(`<a href="%s%s?query=%s">[Web]</a> `, endpoint, rule.Spec.Database, url)
	link += fmt.Sprintf(`<a href="%s%s?query=%s&web=0">[Desktop]</a>`, endpoint, rule.Spec.Database, url)

	summary := fmt.Sprintf("%s<br/><br/>%s</br><pre>%s</pre>", res.Summary, link, query)
	summary = strings.TrimSpace(summary)

	if res.CorrelationID != "" && !strings.HasPrefix(res.CorrelationID, fmt.Sprintf("%s/%s://", rule.Namespace, rule.Name)) {
		res.CorrelationID = fmt.Sprintf("%s/%s://%s", rule.Namespace, rule.Name, res.CorrelationID)
	}

	a := alert.Alert{
		Destination:   rule.Spec.RoutingID,
		Title:         res.Title,
		Summary:       summary,
		Description:   res.Description,
		Severity:      int(res.Severity),
		Source:        fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
		CorrelationID: res.CorrelationID,
		CustomFields:  res.CustomFields,
	}

	addr := fmt.Sprintf("%s/alerts", r.AlertAddr)
	//logger.Info("Sending alert %s %v", addr, a)
	if err := r.AlertCli.Create(context.Background(), addr, a); err != nil {
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
