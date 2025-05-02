package export

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/tinylib/msgp/msgp"
)

type LogToFluentExporterOpts struct {
	// Destination is the fluent protocol destination.
	// It is a string of the form "tcp://host:port" or "unix:///path/to/socket"
	Destination string

	// TagAttribute is the attribute key to extract the fluent tag from.
	// If the attribute within a log is not set, the log will be ignored.
	TagAttribute string
}

// LogToFluentExporter exports logs using the fluent-forward protocol
type LogToFluentExporter struct {
	destinationNetwork string
	destination        string
	encoderPool        *sync.Pool

	conn net.Conn
}

func NewLogToFluentExporter(opts LogToFluentExporterOpts) (*LogToFluentExporter, error) {
	var destination string
	var destinationNetwork string
	if strings.HasPrefix(opts.Destination, "unix://") {
		destination = strings.TrimPrefix(opts.Destination, "unix://")
		destinationNetwork = "unix"
	} else if strings.HasPrefix(opts.Destination, "tcp://") {
		destination = strings.TrimPrefix(opts.Destination, "tcp://")
		destinationNetwork = "tcp"
	} else {
		return nil, fmt.Errorf("invalid destination %s: must be in the form tcp://<host>:<port> or unix:///path/to/socket", opts.Destination)
	}

	if opts.TagAttribute == "" {
		return nil, fmt.Errorf("TagAttribute must be set")
	}

	return &LogToFluentExporter{
		destinationNetwork: destinationNetwork,
		destination:        destination,
		encoderPool: &sync.Pool{
			New: func() interface{} {
				return newFluentEncoder(opts.TagAttribute)
			},
		},
	}, nil
}

func (e *LogToFluentExporter) Open(ctx context.Context) error {
	return nil
}

func (e *LogToFluentExporter) Close() error {
	return nil
}

func (e *LogToFluentExporter) Send(ctx context.Context, batch *types.LogBatch) error {
	encoder := e.encoderPool.Get().(*fluentEncoder)
	defer e.encoderPool.Put(encoder)

	data, err := encoder.encode(batch)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return nil // No logs to send
	}

	conn, err := e.dial(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", e.destination, err)
	}
	defer conn.Close()

	// Set write deadline
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetWriteDeadline(deadline)
	} else {
		conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	}

	// Write the data
	_, err = conn.Write(data)
	if err != nil {
		metrics.CollectorExporterFailed.WithLabelValues(e.Name(), e.destination).Add(float64(len(batch.Logs)))
		return fmt.Errorf("failed to write to %s: %w", e.destination, err)
	}
	metrics.CollectorExporterSent.WithLabelValues(e.Name(), e.destination).Add(float64(len(batch.Logs)))
	return nil
}

func (e *LogToFluentExporter) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	d.Timeout = 10 * time.Second // Adjust timeout as needed

	return d.DialContext(ctx, e.destinationNetwork, e.destination)
}

func (e *LogToFluentExporter) Name() string {
	return "LogToFluentExporter"
}

// fluentEncoder encodes logs in the fluentd forward format
type fluentEncoder struct {
	fluentExtTime *fluentExtTime
	w             []byte
	batchToSend   []*types.Log
	tagAttribute  string
}

func newFluentEncoder(tagAttribute string) *fluentEncoder {
	return &fluentEncoder{
		fluentExtTime: &fluentExtTime{},
		tagAttribute:  tagAttribute,
	}
}

// encode encodes a log batch into the fluentd forward format.
// Not multi-goroutine safe. Returned bytes are only valid until the next call.
func (e *fluentEncoder) encode(batch *types.LogBatch) ([]byte, error) {
	// Copy logbatch elements into new slice so we can sort it without modifying the original,
	// which is shared amongst other outputs.
	e.batchToSend = e.batchToSend[:0]

	for _, log := range batch.Logs {
		e.addBatchToSend(log)
	}
	if len(e.batchToSend) == 0 {
		return nil, nil
	}

	// Sort based on tags. This allows us to create a message per tag.
	slices.SortFunc(e.batchToSend, func(a, b *types.Log) int {
		tagOne := types.StringOrEmpty(a.GetAttributeValue(e.tagAttribute))
		tagTwo := types.StringOrEmpty(b.GetAttributeValue(e.tagAttribute))
		return strings.Compare(tagOne, tagTwo)
	})

	e.w = e.w[:0]

	var err error
	activeTag := ""
	activeTagStart := 0
	for idx, log := range e.batchToSend {
		currTag := types.StringOrEmpty(log.GetAttributeValue(e.tagAttribute))
		if idx == 0 {
			activeTag = currTag
		}

		if idx == len(e.batchToSend)-1 {
			err = e.appendMsg(e.batchToSend[activeTagStart:], activeTag)
			if err != nil {
				return nil, err // bail - message is now invalid.
			}
			return e.w, nil
		}

		peekTag := types.StringOrEmpty(e.batchToSend[idx+1].GetAttributeValue(e.tagAttribute))
		if peekTag != activeTag {
			err = e.appendMsg(e.batchToSend[activeTagStart:idx+1], activeTag)
			if err != nil {
				return nil, err // bail - message is now invalid.
			}
			activeTag = peekTag
			activeTagStart = idx + 1
		}
	}

	return e.w, nil
}

func (e *fluentEncoder) addBatchToSend(log *types.Log) {
	attr := types.StringOrEmpty(log.GetAttributeValue(e.tagAttribute))
	if attr == "" {
		return
	}
	e.batchToSend = append(e.batchToSend, log)
}

func (e *fluentEncoder) appendMsg(batchToSend []*types.Log, tag string) error {
	// Write Forward mode. See https://github.com/fluent/fluentd/wiki/Forward-Protocol-Specification-v1#forward-mode

	// We use the []byte oriented API instead of the writer-oriented API from msgp.
	// the Writer oriented one uses unsafe to slightly speed things up, but we have no reason to use this.
	// Additionally, the writer api does its own buffering and seems overly complicated for what we need.
	var err error
	e.w = msgp.AppendArrayHeader(e.w, 2)                        // [<tag>, <entries>]
	e.w = msgp.AppendString(e.w, tag)                           // <tag>
	e.w = msgp.AppendArrayHeader(e.w, uint32(len(batchToSend))) // <entries>
	for _, log := range batchToSend {
		e.w = msgp.AppendArrayHeader(e.w, 2) // [<time>, <record>]
		e.fluentExtTime.UnixTsNano = log.GetTimestamp()
		e.w, err = msgp.AppendExtension(e.w, e.fluentExtTime) // <time>
		if err != nil {
			return err // need to bail, message is now invalid.
		}

		e.w = msgp.AppendMapHeader(e.w, uint32(log.BodyLen())) // <record>

		err = log.ForEachBody(e.AppendMapElement)
		if err != nil {
			return err // need to bail, message is now invalid.
		}
	}
	return nil
}

func (e *fluentEncoder) AppendMapElement(key string, val any) error {
	var err error
	e.w = msgp.AppendString(e.w, key)
	e.w, err = msgp.AppendIntf(e.w, val)
	if err != nil {
		return err
	}
	return nil
}

// fluentExtTime implements msgp.Extension for the fluent extension timestamp.
// https://github.com/fluent/fluentd/wiki/Forward-Protocol-Specification-v1#eventtime-ext-format
type fluentExtTime struct {
	UnixTsNano uint64
}

func (e *fluentExtTime) ExtensionType() int8 {
	return 0
}

func (e *fluentExtTime) Len() int {
	return 8
}

func (e *fluentExtTime) MarshalBinaryTo(b []byte) error {
	if len(b) < 8 {
		return fmt.Errorf("buffer too small")
	}
	seconds := uint32(e.UnixTsNano / 1e9)
	nanoSeconds := uint32(e.UnixTsNano % 1e9)
	binary.BigEndian.PutUint32(b, uint32(seconds))
	binary.BigEndian.PutUint32(b[4:], uint32(nanoSeconds))
	return nil
}

func (e *fluentExtTime) UnmarshalBinary(b []byte) error {
	if len(b) < 8 {
		return fmt.Errorf("buffer too small")
	}
	seconds := binary.BigEndian.Uint32(b)
	nanoSeconds := binary.BigEndian.Uint32(b[4:])
	e.UnixTsNano = uint64(seconds)*1e9 + uint64(nanoSeconds)
	return nil
}
