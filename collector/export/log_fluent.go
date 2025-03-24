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
	batchToSend   []types.ROLog
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
func (e *fluentEncoder) encode(batch types.ROLogBatch) ([]byte, error) {
	// Copy logbatch elements into new slice so we can sort it without modifying the original,
	// which is shared amongst other outputs.
	e.batchToSend = e.batchToSend[:0]

	batch.ForEach(e.addBatchToSend)
	if len(e.batchToSend) == 0 {
		return nil, nil
	}

	// Sort based on tags. This allows us to create a message per tag.
	slices.SortFunc(e.batchToSend, func(a, b types.ROLog) int {
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

func (e *fluentEncoder) addBatchToSend(log types.ROLog) {
	attr := types.StringOrEmpty(log.GetAttributeValue(e.tagAttribute))
	if attr == "" {
		return
	}
	e.batchToSend = append(e.batchToSend, log)
}

func (e *fluentEncoder) appendMsg(batchToSend []types.ROLog, tag string) error {
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
