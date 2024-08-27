package engine

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"html"
	"strings"
)

// KustoDeepLink returns an encoded string that can be used to create a deep link to a Kusto query.
func KustoDeepLink(q string) (string, error) {
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

// KustoQueryLinks returns a string containing HTML links to the Kusto query in both the web and desktop UI.
func KustoQueryLinks(preText, query, endpoint, database string) (string, error) {
	url, err := KustoDeepLink(query)
	if err != nil {
		return "", fmt.Errorf("failed to create kusto deep link: %w", err)
	}

	if !strings.HasSuffix(endpoint, "/") {
		endpoint = endpoint + "/"
	}

	// Setup the Kusto query deep links
	link := "Execute in "
	link += fmt.Sprintf(`<a href="%s%s?query=%s">[Web]</a> `, endpoint, database, url)
	link += fmt.Sprintf(`<a href="%s%s?query=%s&web=0">[Desktop]</a> `, endpoint, database, url)
	link += fmt.Sprintf(`<a href="%s%s?query=%s&saw=1">[Desktop (SAW)]</a>`, endpoint, database, url)

	escapedQuery := html.EscapeString(query) // Escape html within the kusto query since we are rendering it within html tags
	summary := fmt.Sprintf("%s<br/><br/>%s</br><pre>%s</pre>", preText, link, escapedQuery)
	summary = strings.TrimSpace(summary)
	return summary, nil
}
