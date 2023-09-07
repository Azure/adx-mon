package metrics

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeasureHandlerAndRoundTripper(t *testing.T) {
	testServer := newTestServer()
	client := testServer.Client()
	client.Transport = NewRoundTripper(client.Transport)

	testCases := []struct {
		name             string
		method           string
		statusCode       int
		expectedResponse string
		expectedError    bool
	}{
		{
			name:       "panic",
			method:     "GET",
			statusCode: 500,
		},
		{
			name:             "basic get",
			method:           "GET",
			statusCode:       200,
			expectedResponse: "hello",
		},
		{
			name:             "get with status code 500",
			method:           "GET",
			statusCode:       500,
			expectedResponse: "hello",
		},
		{
			name:             "get with status code 404",
			method:           "GET",
			statusCode:       404,
			expectedResponse: "hello",
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.name), func(t *testing.T) {
			trequire := require.New(t)
			params := url.Values{
				"response":   []string{tc.expectedResponse},
				"statusCode": []string{strconv.Itoa(tc.statusCode)},
			}.Encode()

			switch tc.method {
			case "GET":
				resp, err := client.Get(testServer.URL + "?" + params)

				if tc.expectedError {
					trequire.Error(err)
				} else {
					trequire.NoError(err)
				}

				trequire.Equal(tc.statusCode, resp.StatusCode)

				var buf strings.Builder
				_, err = io.Copy(&buf, resp.Body)
				trequire.NoError(err)
				trequire.Equal(tc.expectedResponse, buf.String())

			default:
				t.Errorf("unsupported method: %s", tc.method)
			}
		})
	}
}

func newTestServer() *httptest.Server {
	f := http.HandlerFunc(HandlerFuncRecorder("fake_system", func(w http.ResponseWriter, r *http.Request) {
		response := r.URL.Query().Get("response")
		statusCode := r.URL.Query().Get("statusCode")

		if statusCode != "0" {
			code, err := strconv.Atoi(statusCode)
			if err != nil {
				panic("invalid status code")
			}
			w.WriteHeader(code)
		}
		if response != "" {
			w.Write([]byte(response))
		}
	}))

	return httptest.NewServer(f)
}
