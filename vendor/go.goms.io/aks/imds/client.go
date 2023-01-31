package imds

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	imdsEndpoint               = "http://169.254.169.254"
	contentTypeHeader          = "Content-Type"
	metadataHeaderKey          = "Metadata"
	metadataHeaderValue        = "true"
	metadataFormatParam        = "format"
	metadataVersionParam       = "api-version"
	jsonFormat                 = "json"
	jsonContentType            = "application/json"
	instanceMetadataAPIVersion = "2019-08-15"
	instanceMetadataPath       = "/metadata/instance"
	scheduledEventsAPIVersion  = "2020-07-01"
	scheduledEventsPath        = "/metadata/scheduledevents"
	acknowledgeEventsPath      = "/metadata/scheduledevents"
	tokenPath                  = "/metadata/identity/oauth2/token"
	tokenAPIVersion            = "2018-02-01"
	userAgentHeader            = "User-Agent"
)

var (
	defaultTimeout                = 5 * time.Second
	defaultScheduledEventsTimeout = 5 * time.Minute
)

type UserAgentConfig struct {
	// Application is the name of the application communicating with this library.
	Application string

	// Version is the version of the application communicating with this library.
	Version string

	// Extra is additional data sent along in the User-Agent header.
	Extra []string
}

func (c *UserAgentConfig) String() string {
	if c.Application == "" {
		c.Application = "unknown"
	}

	if c.Version == "" {
		c.Version = "?"
	}

	if c.Extra == nil {
		c.Extra = make([]string, 0)
	}

	appAndVersion := fmt.Sprintf("%s/%s", c.Application, c.Version)

	res := fmt.Sprintf("aks-imds/? (%s", appAndVersion)
	if len(c.Extra) > 0 {
		res = res + "; " + strings.Join(c.Extra, "; ")
	}

	res += ")"

	return res
}

type ClientOptions struct {
	Endpoint                   string
	InstanceMetadataAPIVersion string
	ScheduledEventsAPIVersion  string
	TokenAPIVersion            string
	Timeout                    time.Duration
	UserAgent                  string
}

type SetClientOption func(o *ClientOptions)

func WithEndpoint(endpoint string) SetClientOption {
	return func(o *ClientOptions) {
		o.Endpoint = endpoint
	}
}

func WithInstanceMetadataAPIVersion(v string) SetClientOption {
	return func(o *ClientOptions) {
		o.InstanceMetadataAPIVersion = v
	}
}

func WithTimeout(t time.Duration) SetClientOption {
	return func(o *ClientOptions) {
		o.Timeout = t
	}
}

func WithUserAgent(c UserAgentConfig) SetClientOption {
	return func(o *ClientOptions) {
		o.UserAgent = c.String()
	}
}

type Client interface {
	AcknowledgeEvents(eventIDs ...string) error
	GetInstanceMetadata() (*InstanceMetadata, error)
	GetScheduledEvents() (*ScheduledEvents, error)
	GetToken(resource string, options *TokenRequestOptions) (*Token, error)
	ResolveManagedIdentityID(resource, resourceID string) (string, error)
	IsAvailable(timeout time.Duration) (bool, error)
}

func NewClient(opts ...SetClientOption) Client {
	client := &client{
		ClientOptions: &ClientOptions{
			Endpoint:                   imdsEndpoint,
			InstanceMetadataAPIVersion: instanceMetadataAPIVersion,
			ScheduledEventsAPIVersion:  scheduledEventsAPIVersion,
			TokenAPIVersion:            tokenAPIVersion,
			Timeout:                    defaultTimeout,
		},
	}

	for _, opt := range opts {
		opt(client.ClientOptions)
	}

	return client
}

type client struct {
	*ClientOptions
}

// IsAvailable checks to see if the IMDS endpoint is available. The timeout provided is for a client->server connection
// dial timeout. If the timeout is reached then the IMDS service is considered unavailable. This method is not intended
// to be fool-proof because it merely tests to see if it can establish a connection on the IMDS endpoint. If another
// service is unintentionally running and allowing connections at the specified address then the check WILL succeed. It
// is generally safe to ignore the error returned from this method and rely only on the boolean value.
func (c *client) IsAvailable(timeout time.Duration) (bool, error) {
	request := &request{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext:       (&net.Dialer{Timeout: timeout}).DialContext,
				DisableKeepAlives: true,
			},
		},
		endpoint: c.Endpoint,
		method:   http.MethodGet,
		path:     instanceMetadataPath,
		queryParams: map[string]string{
			metadataFormatParam:  jsonFormat,
			metadataVersionParam: instanceMetadataAPIVersion,
		},
		headers: map[string]string{
			userAgentHeader: c.UserAgent,
		},
	}

	resp, err := request.execute()
	if err != nil {
		netErr, ok := err.(net.Error)
		if !ok {
			return false, err
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

func (c *client) AcknowledgeEvents(eventIDs ...string) error {
	if len(eventIDs) == 0 {
		return nil
	}

	request := &request{
		httpClient: &http.Client{},
		endpoint:   c.Endpoint,
		method:     http.MethodPost,
		path:       acknowledgeEventsPath,
		queryParams: map[string]string{
			metadataVersionParam: c.ScheduledEventsAPIVersion,
		},
		headers: map[string]string{
			contentTypeHeader: jsonContentType,
		},
	}

	req := &StartRequests{StartRequests: make([]StartRequest, 0)}
	for _, eventID := range eventIDs {
		req.StartRequests = append(req.StartRequests, StartRequest{EventID: eventID})
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return err
	}

	request.body = reqBody

	resp, err := request.execute()
	if err != nil {
		return err
	}

	if _, err := handleResponse(resp); err != nil {
		return err
	}

	return nil
}

func (c *client) GetInstanceMetadata() (*InstanceMetadata, error) {
	request := &request{
		httpClient: &http.Client{
			Timeout: c.Timeout,
		},
		endpoint: c.Endpoint,
		method:   http.MethodGet,
		path:     instanceMetadataPath,
		queryParams: map[string]string{
			metadataFormatParam:  jsonFormat,
			metadataVersionParam: c.InstanceMetadataAPIVersion,
		},
		headers: map[string]string{
			userAgentHeader: c.UserAgent,
		},
	}

	resp, err := request.execute()
	if err != nil {
		return nil, err
	}

	body, err := handleResponse(resp)
	if err != nil {
		return nil, err
	}

	res := &InstanceMetadata{}

	if err := json.Unmarshal(body, res); err != nil {
		return nil, err
	}

	return res, nil
}

func (c *client) GetScheduledEvents() (*ScheduledEvents, error) {
	request := &request{
		httpClient: &http.Client{
			// NOTE <phlombar@microsoft.com> 2020-03-29
			// ----------------------------------------
			// the IMDS schedule events backend is "odd" in that the first request to the service is expected to take
			// a very long time to complete because Azure does some JIT provisioning and setup behind the scenes
			// to ensure events are routed to the right IMDS host. Therefore timeout logic needs to somewhat dumb here
			// compared to other IMDS API calls. I have hard-coded for a 5 minute timeout. In the future we can explore
			// tracking the connection attempts to IMDS and if attempts Succeeded > 1 then switch to using the
			// configured timeout.
			Timeout: defaultScheduledEventsTimeout,
		},
		endpoint: c.Endpoint,
		method:   http.MethodGet,
		path:     scheduledEventsPath,
		queryParams: map[string]string{
			metadataFormatParam:  jsonFormat,
			metadataVersionParam: c.ScheduledEventsAPIVersion,
		},
	}

	resp, err := request.execute()
	if err != nil {
		return nil, err
	}

	body, err := handleResponse(resp)
	if err != nil {
		return nil, err
	}

	res := &ScheduledEvents{}

	if err := json.Unmarshal(body, res); err != nil {
		return nil, err
	}

	return res, nil
}

func (c *client) GetToken(resource string, opts *TokenRequestOptions) (*Token, error) {
	if opts == nil {
		opts = &TokenRequestOptions{}
	}

	queryParams := map[string]string{
		metadataVersionParam: c.TokenAPIVersion,
		"resource":           resource,
	}

	if opts.ManagedIdentityClientID != "" {
		queryParams["client_id"] = opts.ManagedIdentityClientID
	}

	if opts.ManagedIdentityObjectID != "" {
		queryParams["object_id"] = opts.ManagedIdentityObjectID
	}

	if opts.ManagedIdentityResourceID != "" {
		queryParams["mi_res_id"] = opts.ManagedIdentityResourceID
	}

	request := &request{
		httpClient: &http.Client{
			Timeout: c.Timeout,
		},
		endpoint:    c.Endpoint,
		method:      http.MethodGet,
		path:        tokenPath,
		queryParams: queryParams,
		headers: map[string]string{
			userAgentHeader: c.UserAgent,
		},
	}

	resp, err := request.execute()
	if err != nil {
		return nil, err
	}

	body, err := handleResponse(resp)
	if err != nil {
		return nil, err
	}

	res := &Token{}

	if err := json.Unmarshal(body, res); err != nil {
		return nil, err
	}

	return res, nil
}

func (c *client) ResolveManagedIdentityID(resource, resourceID string) (string, error) {
	tok, err := c.GetToken(resource, &TokenRequestOptions{ManagedIdentityResourceID: resourceID})
	if err != nil {
		return "", err
	}

	// NOTE: <phlombar@microsoft.com> 2020-10-09
	// not sure if or when this could occur, but I don't trust Azure to ever do the right thing so provide a useful
	// error for the unfortunate human debugger
	if tok.ClientID == "" {
		return "", fmt.Errorf("failed to resolve managed identity ID using resource ID: %s", resourceID)
	}

	return tok.ClientID, nil
}

type request struct {
	httpClient  *http.Client
	endpoint    string
	method      string
	path        string
	queryParams map[string]string
	headers     map[string]string
	body        []byte
}

func (r *request) prepare() (*http.Request, error) {
	var (
		req *http.Request
		err error
	)

	reqURL := r.endpoint + r.path

	if r.body != nil {
		req, err = http.NewRequest(r.method, reqURL, bytes.NewBuffer(r.body))
	} else {
		req, err = http.NewRequest(r.method, reqURL, nil)
	}

	if err != nil {
		return nil, err
	}

	for k, v := range r.headers {
		req.Header.Set(k, v)
	}

	req.Header.Set(metadataHeaderKey, metadataHeaderValue)

	query := req.URL.Query()

	for k, v := range r.queryParams {
		query.Set(k, v)
	}

	req.URL.RawQuery = query.Encode()

	return req, err
}

func (r *request) execute() (*http.Response, error) {
	req, err := r.prepare()
	if err != nil {
		return nil, err
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func handleResponse(resp *http.Response) (body []byte, err error) {
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.ContentLength != 0 {
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return
		}
	}

	if resp.StatusCode != http.StatusOK {
		return body, errors.New(string(body))
	}

	return
}
