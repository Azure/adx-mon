package clickhouse

import (
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type batchWriter interface {
	Append(values ...any) error
	Flush() error
	Send() error
	Close() error
}

type clickhouseBatch struct {
	batch driver.Batch
}

func (b *clickhouseBatch) Append(values ...any) error {
	return b.batch.Append(values...)
}

func (b *clickhouseBatch) Flush() error {
	return b.batch.Flush()
}

func (b *clickhouseBatch) Send() error {
	return b.batch.Send()
}

func (b *clickhouseBatch) Close() error {
	return b.batch.Close()
}
