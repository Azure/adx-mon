// mock-ingestor is a destructive local load-test sink: a 202 causes the
// collector to delete its WAL even though this process persists nothing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	c, err := parseConfig(args, output)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", c.listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return serve(ctx, listener, c, log.New(output, "mock-ingestor: ", log.LstdFlags))
}

func serve(ctx context.Context, listener net.Listener, c config, logger *log.Logger) error {
	defer listener.Close()
	m := newMockIngestor(c, newRandomPolicy(c), logger)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	server := &http.Server{Handler: m, Protocols: protocols, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	logger.Printf("WARNING: ALL accepted data is DISCARDED; successful 202 acknowledgments cause collector WAL DELETION. No storage, TLS, or authentication; local testing only.")
	logger.Printf("listen=%s protocol=HTTP/1.1 accept_rate=%g disconnect_rate=%g reject_rate=%g overload_scope=%s retry_after=%ss response_delay=%s seed=%d", listener.Addr(), c.acceptRate, c.disconnectRate, 1-(c.acceptRate+c.disconnectRate), c.overloadScope, c.retrySeconds(), c.responseDelay, c.seed)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	ticker := time.NewTicker(c.statsInterval)
	defer ticker.Stop()
	previous, since := m.stats.snapshot(), time.Now()
	var serveErr error
	var serveReturned bool
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case serveErr = <-served:
			serveReturned = true
			break loop
		case <-ticker.C:
			previous = m.stats.report(logger, "periodic", previous, since)
			since = time.Now()
		}
	}
	m.stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), c.shutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	m.handlers.Wait()
	if !serveReturned {
		serveErr = <-served
	}
	m.stats.report(logger, "final", snapshot{}, m.stats.started)
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", serveErr)
	}
	return shutdownErr
}
