package collector

import (
	"compress/gzip"
	"fmt"
	prom_model "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"net"
	"net/http"
	"time"
)

var dialer = &net.Dialer{
	Timeout: 5 * time.Second,
}

var transport = &http.Transport{
	DialContext:         dialer.DialContext,
	TLSHandshakeTimeout: 5 * time.Second,
}
var httpClient = &http.Client{
	Timeout:   time.Second * 10,
	Transport: transport,
}

func FetchMetrics(target string) (map[string]*prom_model.MetricFamily, error) {
	parser := &expfmt.TextParser{}

	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", target, err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("collect node metrics for %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("collect node metrics for %s: %s", target, resp.Status)
	}

	br := resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		br, err = gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("collect node metrics for %s: %w", target, err)
		}
		defer br.Close()
	}

	fams, err := parser.TextToMetricFamilies(br)
	if err != nil {
		return nil, fmt.Errorf("decode metrics for %s: %w", target, err)
	}
	return fams, err
}
