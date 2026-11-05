package config

import (
	"fmt"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

func TestStaticScrapeInterval(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        time.Duration
		wantErr     string
	}{
		{name: "omitted"},
		{name: "seconds", field: `, scrape-interval = '60s'`, want: time.Minute},
		{name: "subsecond", field: `, scrape-interval = '500ms'`, want: 500 * time.Millisecond},
		{name: "compound", field: `, scrape-interval = '1m30s'`, want: 90 * time.Second},
		{name: "empty", field: `, scrape-interval = ''`, wantErr: `scrape-interval "" must be a positive Go duration`},
		{name: "invalid", field: `, scrape-interval = 'later'`, wantErr: `scrape-interval "later" must be a positive Go duration`},
		{name: "unitless", field: `, scrape-interval = '60'`, wantErr: `scrape-interval "60" must be a positive Go duration`},
		{name: "zero", field: `, scrape-interval = '0s'`, wantErr: `scrape-interval "0s" must be greater than 0`},
		{name: "negative", field: `, scrape-interval = '-1s'`, wantErr: `scrape-interval "-1s" must be greater than 0`},
		{name: "overflow", field: `, scrape-interval = '999999999999h'`, wantErr: `scrape-interval "999999999999h" must be a positive Go duration`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf(`[prometheus-scrape]
database = 'Metrics'
scrape-interval = 30
static-scrape-target = [
  { host-regex = '.*', url = 'http://localhost:3100/metrics', namespace = 'adx-mon', pod = 'collector', container = 'collector'%s }
]`, tc.field)
			var cfg Config
			require.NoError(t, toml.Unmarshal([]byte(input), &cfg))
			err := cfg.PrometheusScrape.Validate()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, "prom-scrape.static-scrape-target[0]."+tc.wantErr)
				return
			}
			require.NoError(t, err)
			target := cfg.PrometheusScrape.StaticScrapeTarget[0]
			interval, err := target.ParseScrapeInterval()
			require.NoError(t, err)
			require.Equal(t, tc.want, interval)
			require.Equal(t, 30, cfg.PrometheusScrape.ScrapeIntervalSeconds)
			encoded, err := toml.Marshal(target)
			require.NoError(t, err)
			if tc.field == "" {
				require.Nil(t, target.ScrapeInterval)
				require.NotContains(t, string(encoded), "scrape-interval")
			}
			var roundTrip ScrapeTarget
			require.NoError(t, toml.Unmarshal(encoded, &roundTrip))
			require.Equal(t, *target, roundTrip)
		})
	}
}

func TestStaticScrapeIntervalRequiresString(t *testing.T) {
	var target ScrapeTarget
	require.Error(t, toml.Unmarshal([]byte("scrape-interval = 60"), &target))
}
