package azauth

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultIMDSHost = "169.254.169.254"
)

var (
	defaultTimeout = 2 * time.Second
)

// isIMDSAvailable opens a connection to the specified server. If the connection attempt results in a timeout then
// false is returned without an error. If the connection does not timeout then true is returned without an error. Any
// other error generated during the IMDS availability check is returned to the caller.
func isIMDSAvailable(host string, timeout time.Duration) (bool, error) {
	if host == "" {
		host = defaultIMDSHost
	}

	if timeout == 0 {
		timeout = defaultTimeout
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext:       (&net.Dialer{Timeout: timeout}).DialContext,
			DisableKeepAlives: true,
		},
	}

	if strings.HasPrefix(host, "http://") {
		host = strings.TrimPrefix(host, "http://")
	}

	imdsURL := fmt.Sprintf("http://%s/metadata/instance", host)

	if _, err := url.Parse(imdsURL); err != nil {
		return false, err
	}

	req, err := http.NewRequest(http.MethodGet, imdsURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create imds http request: %w", err)
	}

	req.Header.Set("Metadata", "true")

	resp, err := client.Do(req)
	if err != nil {
		netErr, ok := err.(net.Error)
		if !ok {
			return false, fmt.Errorf("failed to receive imds http response: %w", netErr)
		}

		if netErr.Timeout() {
			return false, nil
		}

		return false, err
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	return true, nil
}
