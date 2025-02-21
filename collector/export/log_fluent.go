package export

import (
	"context"
	"encoding/binary"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/tinylib/msgp/msgp"
)

// LogToFluentExporter exports logs using the fluent-forward protocol
type LogToFluentExporter struct {
	destination string
}

type LogToFluentExporterOpts struct {
	Destination string
}

func (e *LogToFluentExporter) Send(ctx context.Context, batch *types.LogBatch) error {
	return nil
}

// fluentEncoder encodes logs in the fluentd forward format
// Not goroutine safe. Returned bytes are only valid until the next call
type fluentEncoder struct {
	w             []byte
	fluentExtTime *fluentExtTime
}

func newFluentEncoder() *fluentEncoder {
	return &fluentEncoder{
		fluentExtTime: &fluentExtTime{},
	}
}

// Encode encodes a log batch into the fluentd forward format
// TODO - limit message sizes to limit total byte size.
//   - Do not write as we go along unless we are very careful to avoid
//     partial messages on the wire, which will upset mdsd.
func (e *fluentEncoder) Encode(batch *types.LogBatch) ([]byte, error) {
	// Write Forward mode. See https://github.com/fluent/fluentd/wiki/Forward-Protocol-Specification-v1#forward-mode
	e.w = e.w[:0]
	// We use the []byte oriented API instead of the writer-oriented API from msgp.
	// the Writer oriented one uses unsafe to slightly speed things up, but we have no reason to use this.
	// Additionally, the writer api does its own buffering and seems overly complicated for what we need.
	e.w = msgp.AppendArrayHeader(e.w, 2)    // [<tag>, <entries>]
	e.w = msgp.AppendString(e.w, "tag-val") // <tag> TODO split on these
	e.w = msgp.AppendArrayHeader(e.w, uint32(len(batch.Logs)))

	var err error
	for _, log := range batch.Logs {
		// split based on destination tag
		e.w = msgp.AppendArrayHeader(e.w, 2) // [<time>, <record>]
		e.fluentExtTime.UnixTsNano = log.Timestamp
		e.w, err = msgp.AppendExtension(e.w, e.fluentExtTime) // <time>
		if err != nil {
			return nil, err // TODO - skip???
		}
		// TODO - also put in resource/attributes?
		e.w, err = msgp.AppendMapStrIntf(e.w, log.Body)
		if err != nil {
			return nil, err // TODO - skip???
		}
	}

	return e.w, nil
}

type fluentExtTime struct {
	UnixTsNano uint64
}

func (e *fluentExtTime) ExtensionType() int8 {
	return 0
}

func (e *fluentExtTime) Len() int {
	return 8
}

// https://github.com/fluent/fluentd/wiki/Forward-Protocol-Specification-v1#eventtime-ext-format
func (e *fluentExtTime) MarshalBinaryTo(b []byte) error {
	seconds := uint32(e.UnixTsNano / 1_000_000_000)
	nanoSeconds := uint32(e.UnixTsNano % 1_000_000_000)
	binary.BigEndian.PutUint32(b, uint32(seconds))
	binary.BigEndian.PutUint32(b[4:], uint32(nanoSeconds))
	return nil
}

func (e *fluentExtTime) UnmarshalBinary(b []byte) error {
	seconds := binary.BigEndian.Uint32(b)
	nanoSeconds := binary.BigEndian.Uint32(b[4:])
	e.UnixTsNano = uint64(seconds)*1_000_000_000 + uint64(nanoSeconds)
	return nil
}
