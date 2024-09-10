package ingestor

import (
	"net"
	"sync"

	"github.com/Azure/adx-mon/metrics"
)

// LimitListener returns a Listener that accepts at most n simultaneous
// connections from the provided Listener and will drop extra connections.
func LimitListener(l net.Listener, n int) net.Listener {
	return &limitListener{Listener: l, sem: make(chan struct{}, n)}
}

// limitListener is a listener that limits the number of active connections
// at any given time.
type limitListener struct {
	net.Listener
	sem chan struct{}
}

func (l *limitListener) release() {
	<-l.sem
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		select {
		case l.sem <- struct{}{}:
			metrics.IngestorActiveConnections.Inc()
			return &limitListenerConn{Conn: c, release: l.release}, nil
		default:
			metrics.IngestorDroppedConnectionsTotal.Inc()
			c.Close()
		}
	}
}

type limitListenerConn struct {
	net.Conn
	releaseOnce sync.Once
	release     func()
}

func (l *limitListenerConn) Close() error {
	err := l.Conn.Close()
	l.releaseOnce.Do(l.release)
	metrics.IngestorActiveConnections.Dec()
	return err
}
