package clickhouse

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
)

// connectionManager abstracts the client lifecycle so the uploader can focus on
// higher-level batching concerns.
type connectionManager interface {
	Ping(ctx context.Context) error
	Close() error
	PrepareInsert(ctx context.Context, database, table string, columns []Column) (batchWriter, error)
	Exec(ctx context.Context, query string, args ...any) error
}

var newConnectionManager = func(cfg Config) (connectionManager, error) {
	opts, err := buildOptions(cfg)
	if err != nil {
		return nil, err
	}
	return &clientPool{opts: opts}, nil
}

// clientPool lazily dials ClickHouse and keeps a single shared connection for
// the uploader. Future phases can expand this to a full pool if needed.
type clientPool struct {
	mu   sync.Mutex
	conn clickhouse.Conn
	opts *clickhouse.Options
}

func (p *clientPool) ensureConn(ctx context.Context) (clickhouse.Conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		return p.conn, nil
	}

	conn, err := clickhouse.Open(p.opts)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	p.conn = conn
	return conn, nil
}

func (p *clientPool) Ping(ctx context.Context) error {
	_, err := p.ensureConn(ctx)
	return err
}

func (p *clientPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return nil
	}
	err := p.conn.Close()
	p.conn = nil
	return err
}

func (p *clientPool) PrepareInsert(ctx context.Context, database, table string, columns []Column) (batchWriter, error) {
	conn, err := p.ensureConn(ctx)
	if err != nil {
		return nil, err
	}

	query := buildInsertQuery(database, table, columns)
	batch, err := conn.PrepareBatch(ctx, query)
	if err != nil {
		return nil, err
	}

	return &clickhouseBatch{batch: batch}, nil
}

func (p *clientPool) Exec(ctx context.Context, query string, args ...any) error {
	conn, err := p.ensureConn(ctx)
	if err != nil {
		return err
	}
	return conn.Exec(ctx, query, args...)
}

func buildOptions(cfg Config) (*clickhouse.Options, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("dsn is required")
	}

	uri, err := url.Parse(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	if uri.Host == "" {
		return nil, errors.New("dsn missing host")
	}

	protocol, defaultPort, err := resolveProtocol(uri.Scheme)
	if err != nil {
		return nil, err
	}

	addresses := expandAddresses(uri.Host, defaultPort)
	tlsConfig, err := buildTLSConfig(cfg.TLS, uri)
	if err != nil {
		return nil, err
	}

	username, password := resolveAuth(uri, cfg.Auth)
	compression, err := resolveCompression(cfg.Client.Compression.Method)
	if err != nil {
		return nil, err
	}

	settings := clickhouse.Settings{}
	if cfg.Client.AsyncInsert {
		settings["async_insert"] = 1
		settings["wait_for_async_insert"] = 1
	}

	return &clickhouse.Options{
		Protocol:    protocol,
		Addr:        addresses,
		Auth:        clickhouse.Auth{Database: cfg.Database, Username: username, Password: password},
		TLS:         tlsConfig,
		DialTimeout: cfg.Client.DialTimeout,
		ReadTimeout: cfg.Client.ReadTimeout,
		Compression: compression,
		Settings:    settings,
	}, nil
}

func resolveProtocol(scheme string) (clickhouse.Protocol, string, error) {
	switch strings.ToLower(scheme) {
	case "", "clickhouse", "native", "tcp":
		return clickhouse.Native, "9000", nil
	case "https":
		return clickhouse.HTTP, "8443", nil
	case "http":
		return clickhouse.HTTP, "8123", nil
	default:
		return 0, "", fmt.Errorf("unsupported clickhouse scheme: %s", scheme)
	}
}

func expandAddresses(hostPart, defaultPort string) []string {
	hosts := strings.Split(hostPart, ",")
	addresses := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(host, defaultPort)
		}
		addresses = append(addresses, host)
	}
	return addresses
}

func buildTLSConfig(cfg TLSConfig, uri *url.URL) (*tls.Config, error) {
	secure := false
	if rawSecure := uri.Query().Get("secure"); rawSecure != "" {
		if parsed, err := strconv.ParseBool(rawSecure); err == nil {
			secure = parsed
		}
	}

	scheme := strings.ToLower(uri.Scheme)
	hasUserTLS := cfg.CAFile != "" || (cfg.CertFile != "" && cfg.KeyFile != "") || cfg.InsecureSkipVerify
	needTLS := scheme == "https" || secure || hasUserTLS
	if !needTLS {
		return nil, nil
	}

	tlsCfg := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify}

	if host := uri.Hostname(); host != "" {
		tlsCfg.ServerName = host
	}

	if cfg.CAFile != "" {
		caPEM, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read clickhouse ca file: %w", err)
		}
		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM(caPEM); !ok {
			return nil, errors.New("failed to parse clickhouse ca bundle")
		}
		tlsCfg.RootCAs = pool
	}

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load clickhouse client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if tlsCfg.RootCAs == nil && !cfg.InsecureSkipVerify {
		tlsCfg.MinVersion = tls.VersionTLS12
	}

	return tlsCfg, nil
}

func resolveAuth(uri *url.URL, override AuthConfig) (string, string) {
	username := override.Username
	password := override.Password

	if uri.User != nil {
		if user := uri.User.Username(); user != "" {
			username = user
		}
		if pass, ok := uri.User.Password(); ok {
			password = pass
		}
	}

	if username == "" {
		username = os.Getenv("CLICKHOUSE_USER")
	}
	if password == "" {
		password = os.Getenv("CLICKHOUSE_PASSWORD")
	}

	return username, password
}

func resolveCompression(method string) (*clickhouse.Compression, error) {
	switch strings.ToLower(method) {
	case CompressionNone:
		return &clickhouse.Compression{Method: clickhouse.CompressionNone}, nil
	case CompressionZSTD:
		return &clickhouse.Compression{Method: clickhouse.CompressionZSTD}, nil
	case CompressionLZ4:
		fallthrough
	case "":
		return &clickhouse.Compression{Method: clickhouse.CompressionLZ4}, nil
	default:
		return nil, fmt.Errorf("unsupported compression method: %s", method)
	}
}
