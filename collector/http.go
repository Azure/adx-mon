package collector

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type HttpServerOpts struct {
	ListenAddr string
}

type HttpServer struct {
	mux  *http.ServeMux
	opts *HttpServerOpts
	srv  *http.Server

	cancelFn context.CancelFunc
}

func NewHttpServer(opts *HttpServerOpts) *HttpServer {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return &HttpServer{
		opts: opts,
		mux:  mux,
	}
}

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

func (s *HttpServer) Close() error {
	return s.srv.Shutdown(context.Background())
}

func (s *HttpServer) RegisterHandler(path string, handlerFunc http.HandlerFunc) {
	s.mux.Handle(path, handlerFunc)
}
