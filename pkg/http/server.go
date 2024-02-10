package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/netutil"
)

type HttpHandler struct {
	Path    string
	Handler http.HandlerFunc
}

type ServerOpts struct {
	// MaxConns is the maximum number of connections the server will accept.  This value is only respected if
	// Listener is nil.
	MaxConns int

	// ReadTimeout is the maximum duration before timing out read of the request.
	// If not specified, the default is 5 seconds.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out write of the response.
	// If not specified, the default is 10 seconds.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request when keep-alives are enabled.
	// If not specified, the default is 10 seconds.
	IdleTimeout time.Duration

	// MaxHeaderBytes is the maximum number of bytes the server will read parsing the request header's keys and values,
	// If not specified, the default is 1MB (1 << 20).
	MaxHeaderBytes int

	// ListenAddr is the address to listen on.  If a Listener is provided, this value is ignored.
	ListenAddr string

	// Listener is the listener to use.  If nil, a new listener will be created.
	Listener net.Listener
}

// HttpServer is a http server that is preconfigured with a prometheus metrics handler.  Additional handlers can be
// registered with RegisterHandler.
type HttpServer struct {
	mux  *http.ServeMux
	opts *ServerOpts
	srv  *http.Server

	listener net.Listener
	cancelFn context.CancelFunc
}

func NewServer(opts *ServerOpts) *HttpServer {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return &HttpServer{
		opts: opts,
		mux:  mux,
	}
}

// Open starts the http server.
func (s *HttpServer) Open(ctx context.Context) error {
	s.listener = s.opts.Listener
	if s.opts.Listener == nil {
		var err error
		s.listener, err = net.Listen("tcp", s.opts.ListenAddr)
		if err != nil {
			return err
		}
	}

	if s.opts.MaxConns > 0 {
		s.listener = netutil.LimitListener(s.listener, s.opts.MaxConns)
	}

	readTimeout := 5 * time.Second
	if s.opts.ReadTimeout > 0 {
		readTimeout = s.opts.ReadTimeout
	}

	writeTimeout := 10 * time.Second
	if s.opts.WriteTimeout > 0 {
		writeTimeout = s.opts.WriteTimeout
	}

	idleTimeout := 10 * time.Second
	if s.opts.IdleTimeout > 0 {
		idleTimeout = s.opts.IdleTimeout
	}

	maxHeaderBytes := 1 << 20
	if s.opts.MaxHeaderBytes > 0 {
		maxHeaderBytes = s.opts.MaxHeaderBytes
	}

	s.srv = &http.Server{
		ReadTimeout:    readTimeout,
		WriteTimeout:   writeTimeout,
		IdleTimeout:    idleTimeout,
		MaxHeaderBytes: maxHeaderBytes,
		Addr:           s.opts.ListenAddr,
		Handler:        s.mux}

	go func() {
		if err := s.srv.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()
	return nil
}

// Close shuts down the http server.
func (s *HttpServer) Close() error {
	return s.srv.Shutdown(context.Background())
}

// RegisterHandler registers a new handler at the given path.  The handler must be registered before Open is called.
func (s *HttpServer) RegisterHandler(path string, handlerFunc http.HandlerFunc) {
	s.mux.Handle(path, handlerFunc)
}
