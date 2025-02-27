package export

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"

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
	panic("not implemented")
}

// fluentEncoder encodes logs in the fluentd forward format
type fluentEncoder struct {
	fluentExtTime *fluentExtTime
	w             []byte
	batchToSend   []*types.Log
}

func newFluentEncoder() *fluentEncoder {
	return &fluentEncoder{
		fluentExtTime: &fluentExtTime{},
	}
}

// encode encodes a log batch into the fluentd forward format.
// Not multi-goroutine safe. Returned bytes are only valid until the next call.
func (e *fluentEncoder) encode(tagAttribute string, batch *types.LogBatch) ([]byte, error) {
	// Copy logbatch elements into new slice so we can sort it without modifying the original,
	// which is shared amongst other outputs.
	e.batchToSend = e.batchToSend[:0]
	for _, log := range batch.Logs {
		_, ok := log.Attributes[tagAttribute].(string)
		if !ok {
			continue
		}
		e.batchToSend = append(e.batchToSend, log)
	}
	if len(e.batchToSend) == 0 {
		return nil, nil
	}

	// Sort based on tags. This allows us to create a message per tag.
	slices.SortFunc(e.batchToSend, func(a, b *types.Log) int {
		tagOne := a.Attributes[tagAttribute]
		tagTwo := b.Attributes[tagAttribute]
		return strings.Compare(tagOne.(string), tagTwo.(string))
	})

	e.w = e.w[:0]

	var err error
	activeTag := ""
	activeTagStart := 0
	for idx, log := range e.batchToSend {
		currTag := log.Attributes[tagAttribute].(string)
		if idx == 0 {
			activeTag = currTag
		}

		if idx == len(e.batchToSend)-1 {
			err = e.appendMsg(e.batchToSend[activeTagStart:], activeTag)
			if err != nil {
				return nil, err // skip???
			}
			return e.w, nil
		}

		peekTag := e.batchToSend[idx+1].Attributes[tagAttribute].(string)
		if peekTag != activeTag {
			err = e.appendMsg(e.batchToSend[activeTagStart:idx+1], activeTag)
			if err != nil {
				return nil, err // skip???
			}
			activeTag = peekTag
			activeTagStart = idx + 1
		}
	}

	return e.w, nil
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
		e.fluentExtTime.UnixTsNano = log.Timestamp
		e.w, err = msgp.AppendExtension(e.w, e.fluentExtTime) // <time>
		if err != nil {
			return err // TODO - skip???
		}
		// TODO - also put in resource/attributes?
		e.w, err = msgp.AppendMapStrIntf(e.w, log.Body)
		if err != nil {
			return err // TODO - skip???
		}
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
