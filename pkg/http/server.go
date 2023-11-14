package http

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ServerOpts struct {
	ListenAddr string
}

// HttpServer is a http server that is preconfigured with a prometheus metrics handler.  Additional handlers can be
// registered with RegisterHandler.
type HttpServer struct {
	mux  *http.ServeMux
	opts *ServerOpts
	srv  *http.Server

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
	s.srv = &http.Server{Addr: s.opts.ListenAddr, Handler: s.mux}

	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
