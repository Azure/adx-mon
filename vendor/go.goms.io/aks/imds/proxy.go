package imds

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

type proxyRequest struct {
	*http.Request
	options ProxyOptions
}

func (h *proxyRequest) modifyResponse(r *http.Response) error {
	switch h.URL.Path {
	case "/metadata/instance":
		return h.modifyInstanceMetadata(r)
	default:
		return nil
	}
}

func (h *proxyRequest) modifyInstanceMetadata(r *http.Response) error {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}

	r.Body = ioutil.NopCloser(bytes.NewBuffer(body))

	im := &InstanceMetadata{}
	if err := json.Unmarshal(body, im); err != nil {
		return err
	}

	if h.options.ReplaceEnvironmentName != "" {
		im.Compute.Cloud = h.options.ReplaceEnvironmentName
	}

	modifiedBody, err := json.Marshal(im)
	if err != nil {
		return err
	}

	r.Body = ioutil.NopCloser(bytes.NewBuffer(modifiedBody))

	return nil
}

type director func(*http.Request)

func (f director) then(d director) director {
	return func(req *http.Request) {
		f(req)
		d(req)
	}
}

func hostDirector(backend *url.URL) director {
	return func(req *http.Request) {
		req.Header.Set("X-Forwarded-Host", req.Host)

		req.URL.Host = backend.Host
		req.URL.Scheme = backend.Scheme
		req.Host = backend.Host
	}
}

func NewProxy(opts ProxyOptions) Proxy {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}

	if opts.Backend == "" {
		opts.Backend = imdsEndpoint
	}

	p := &proxy{
		ProxyOptions: opts,
	}

	return p
}

type ProxyOptions struct {
	// Backend is the target host the proxy is forwarding requests to.
	Backend string

	Host string

	// Port is the proxy server's listen
	Port int

	// ReplaceEnvironmentName replaces the value of "azEnvironment" in an IMDS metadata result.
	ReplaceEnvironmentName string
}

type Proxy interface {
	ListenAndServe() error
}

type proxy struct {
	ProxyOptions
}

func (p *proxy) forward(w http.ResponseWriter, r *http.Request) {
	targetURL, _ := url.Parse(p.Backend)

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	d := proxy.Director
	proxy.Director = director(d).then(hostDirector(targetURL))

	// wrap the request so its properties are available when modifying the response, for example, the IMDS request
	// path which controls how a JSON body is decoded.
	proxyRequest := &proxyRequest{Request: r, options: p.ProxyOptions}
	proxy.ModifyResponse = proxyRequest.modifyResponse

	proxy.ServeHTTP(w, r)
}

func (p *proxy) ListenAndServe() error {
	http.HandleFunc("/", p.forward)

	listenAddr := fmt.Sprintf("%s:%d", p.Host, p.Port)
	log.Printf("Starting imds-proxy: http://%s -> %s\n", listenAddr, p.Backend)

	return http.ListenAndServe(listenAddr, nil)
}
