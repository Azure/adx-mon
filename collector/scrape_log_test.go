package collector

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/stretchr/testify/require"
)

// syncBuffer is a bytes.Buffer that is safe for concurrent writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Routine scrapes do not log at info level, which would log every target on every interval.
func TestScraper_RoutineScrapesDoNotLogAtInfo(t *testing.T) {
	out := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previous)

	synctest.Test(t, func(t *testing.T) {
		startTestScraper(t, &ScraperOpts{
			ScrapeInterval: time.Second,
			Targets:        []ScrapeTarget{{Addr: "http://localhost/a", Pod: "a"}, {Addr: "http://localhost/b", Pod: "b"}},
			RemoteClients:  []remote.RemoteWriteClient{&scrapeTestRemote{}},
		}, func(req *http.Request) (*http.Response, error) {
			return scrapeResponse(http.StatusOK), nil
		})
		time.Sleep(10 * time.Second)
		synctest.Wait()
	})

	lines := strings.Count(out.String(), "level=INFO")
	t.Logf("info log lines for 2 targets over 10 scrape intervals: %d", lines)
	require.Zero(t, lines)
}
