package sinks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/tidwall/gjson"
)

type StoreSink struct {
	done        bool
	doneChannel chan struct{}
	store       storage.Store
	lock        sync.Mutex
	logger      *slog.Logger
}

func NewStore(s storage.Store) *StoreSink {
	return &StoreSink{
		doneChannel: make(chan struct{}),
		store:       s,
		logger: slog.Default().With(
			slog.Group(
				"handler",
				slog.String("sink", "store"),
			),
		),
	}
}

func (s *StoreSink) Open(ctx context.Context) error {
	return nil
}

func (s *StoreSink) Send(ctx context.Context, batch *types.LogBatch) error {
	grouped := groupByDestination(batch, s.logger)
	for k, logs := range grouped {
		ss := strings.Split(k, ".")
		if err := s.store.WriteLogs(ctx, ss[0], ss[1], &types.LogBatch{Logs: logs}); err != nil {
			return fmt.Errorf("failed to write logs to store: %w", err)
		}

		if logger.IsDebug() {
			s.logger.Debug("Wrote logs to store", slog.String("database", ss[0]), slog.String("table", ss[1]), slog.Int("count", len(logs)))
		}
	}
	batch.Ack()
	return nil
}

func (s *StoreSink) Close() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if !s.done {
		s.done = true
		close(s.doneChannel)
	}
	return nil
}

func (s *StoreSink) Name() string {
	return "StoreSink"
}

func (s *StoreSink) DoneChan() chan struct{} {
	return s.doneChannel
}

func groupByDestination(batch *types.LogBatch, log *slog.Logger) map[string][]*types.Log {
	var (
		group = make(map[string][]*types.Log)
		key   string
	)
	for _, l := range batch.Logs {

		database := gjson.Get(l.Body["message"].(string), `kusto\.database`).String()
		table := gjson.Get(l.Body["message"].(string), `kusto\.table`).String()
		if database == "" || table == "" {
			log.Warn("Missing kusto metadata", slog.Any("attributes", l.Attributes), slog.Any("body", l.Body))
			continue
		}
		key = database + "." + table
		group[key] = append(group[key], l)
	}
	return group
}
