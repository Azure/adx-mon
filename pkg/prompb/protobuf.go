package prompb

import (
	"fmt"
	mathbits "math/bits"

	"github.com/VictoriaMetrics/easyproto"
)

var (
	mp = &easyproto.MarshalerPool{}

	WriteRequestPool = NewPool[*WriteRequest](
		func() *WriteRequest {
			return &WriteRequest{}
		},
	)

	TimeSeriesPool = NewPool[*TimeSeries](
		func() *TimeSeries {
			return &TimeSeries{}
		},
	)
)

const (
	maxRetainedTimeSeries = 4096
	maxRetainedLabels     = 64
	maxRetainedSamples    = 256
	maxRetainedLabelBytes = 4096
)

// WriteRequest represents Prometheus remote write API request
type WriteRequest struct {
	Timeseries []*TimeSeries
}

// TimeSeries is a timeseries.
type TimeSeries struct {
	Labels  []*Label
	Samples []*Sample
}

// Label is a timeseries label
type Label struct {
	Name  []byte
	Value []byte
}

// Sample is a timeseries sample.
type Sample struct {
	Value     float64
	Timestamp int64
}

// Unmarshal unmarshals m from src.
func (wr *WriteRequest) Unmarshal(src []byte) (err error) {
	wr.Timeseries = wr.Timeseries[:0]
	var fc easyproto.FieldContext
	for len(src) > 0 {
		src, err = fc.NextField(src)
		if err != nil {
			return fmt.Errorf("cannot read next field in Timeseries message")
		}
		switch fc.FieldNum {
		case 1:
			data, ok := fc.MessageData()
			if !ok {
				return fmt.Errorf("cannot read Timeseries sample data")
			}
			if cap(wr.Timeseries) > len(wr.Timeseries) {
				wr.Timeseries = wr.Timeseries[:len(wr.Timeseries)+1]
				wr.Timeseries[len(wr.Timeseries)-1] = TimeSeriesPool.Get()
			} else {
				wr.Timeseries = append(wr.Timeseries, TimeSeriesPool.Get())
			}
			ts := wr.Timeseries[len(wr.Timeseries)-1]
			if err := ts.unmarshalProtobuf(data); err != nil {
				return fmt.Errorf("cannot unmarshal sample: %w", err)
			}
		}
	}
	return nil
}

func (wr *WriteRequest) Marshal() (dAtA []byte, err error) {
	b := make([]byte, wr.Size())
	return wr.MarshalTo(b[:0])
}

func (wr *WriteRequest) MarshalTo(dst []byte) ([]byte, error) {
	marshaller := mp.Get()
	marshaller.Reset()
	mm := marshaller.MessageMarshaler()
	for _, ts := range wr.Timeseries {
		ts.marshalProtobuf(mm.AppendMessage(1))
	}
	dst = marshaller.Marshal(dst[:0])
	mp.Put(marshaller)
	return dst, nil
}

// Reset resets wr.
func (wr *WriteRequest) Reset() {
	for i := range wr.Timeseries {
		ReleaseTimeSeries(wr.Timeseries[i])
	}

	all := wr.Timeseries[:cap(wr.Timeseries)]
	clear(all)
	if cap(all) > maxRetainedTimeSeries {
		wr.Timeseries = nil
		return
	}
	wr.Timeseries = all[:0]
}

func (wr *WriteRequest) Size() (n int) {
	if wr == nil {
		return 0
	}
	var l int
	_ = l
	if len(wr.Timeseries) > 0 {
		for _, e := range wr.Timeseries {
			l = e.Size()
			n += 1 + l + sizeOf(uint64(l))
		}
	}
	return n
}

func (s *Sample) Size() (n int) {
	if s == nil {
		return 0
	}
	var l int
	_ = l
	if s.Value != 0 {
		n += 9
	}
	if s.Timestamp != 0 {
		n += 1 + sizeOf(uint64(s.Timestamp))
	}
	return n
}

func (s *Sample) marshalProtobuf(mm *easyproto.MessageMarshaler) {
	mm.AppendDouble(1, s.Value)
	mm.AppendInt64(2, s.Timestamp)
}

func (s *Sample) unmarshalProtobuf(src []byte) (err error) {
	// Set default Sample values
	s.Value = 0
	s.Timestamp = 0

	// Parse Sample message at src
	var fc easyproto.FieldContext
	for len(src) > 0 {
		src, err = fc.NextField(src)
		if err != nil {
			return fmt.Errorf("cannot read next field in sample")
		}
		switch fc.FieldNum {
		case 1:
			value, ok := fc.Double()
			if !ok {
				return fmt.Errorf("cannot read sample value")
			}
			s.Value = value
		case 2:
			timestamp, ok := fc.Int64()
			if !ok {
				return fmt.Errorf("cannot read sample timestamp")
			}
			s.Timestamp = timestamp
		}
	}
	return nil

}

func (s *Sample) Reset() {
	s.Value = 0
	s.Timestamp = 0
}

func (m *TimeSeries) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if len(m.Labels) > 0 {
		for _, e := range m.Labels {
			l = e.Size()
			n += 1 + l + sizeOf(uint64(l))
		}
	}
	if len(m.Samples) > 0 {
		for _, e := range m.Samples {
			l = e.Size()
			n += 1 + l + sizeOf(uint64(l))
		}
	}
	return n
}

func (m *TimeSeries) marshalProtobuf(mm *easyproto.MessageMarshaler) {
	for _, l := range m.Labels {
		l.marshalProtobuf(mm.AppendMessage(1))
	}
	for _, s := range m.Samples {
		s.marshalProtobuf(mm.AppendMessage(2))
	}
}

func (m *TimeSeries) unmarshalProtobuf(src []byte) (err error) {
	m.Labels = m.Labels[:0]
	m.Samples = m.Samples[:0]

	var fc easyproto.FieldContext
	for len(src) > 0 {
		src, err = fc.NextField(src)
		if err != nil {
			return fmt.Errorf("cannot read next field in Timeseries message")
		}
		switch fc.FieldNum {
		case 1:
			data, ok := fc.MessageData()
			if !ok {
				return fmt.Errorf("cannot read Timeseries sample data")
			}
			s := m.nextLabel()
			if err := s.unmarshalProtobuf(data); err != nil {
				return fmt.Errorf("cannot unmarshal sample: %w", err)
			}
		case 2:
			data, ok := fc.MessageData()
			if !ok {
				return fmt.Errorf("cannot read Timeseries sample data")
			}
			s := m.nextSample()
			if err := s.unmarshalProtobuf(data); err != nil {
				return fmt.Errorf("cannot unmarshal sample: %w", err)
			}
		}
	}
	return nil
}

func (ts *TimeSeries) Reset() {
	labels := ts.Labels[:cap(ts.Labels)]
	for i := range labels {
		if labels[i] != nil {
			labels[i].Reset()
		}
	}
	samples := ts.Samples[:cap(ts.Samples)]
	for i := range samples {
		if samples[i] != nil {
			samples[i].Reset()
		}
	}
	ts.Labels = labels[:0]
	ts.Samples = samples[:0]
}

func (ts *TimeSeries) retainable() bool {
	return cap(ts.Labels) <= maxRetainedLabels && cap(ts.Samples) <= maxRetainedSamples
}

func (ts *TimeSeries) discard() {
	ts.Labels = nil
	ts.Samples = nil
}

func ReleaseTimeSeries(ts *TimeSeries) {
	if ts == nil {
		return
	}
	if ts.retainable() {
		TimeSeriesPool.Put(ts)
		return
	}
	ts.discard()
}

func (m *TimeSeries) nextLabel() *Label {
	if len(m.Labels) < cap(m.Labels) {
		m.Labels = m.Labels[:len(m.Labels)+1]
		if m.Labels[len(m.Labels)-1] == nil {
			m.Labels[len(m.Labels)-1] = &Label{}
		}
		return m.Labels[len(m.Labels)-1]
	}

	l := &Label{}
	m.Labels = append(m.Labels, l)
	return l
}

func (m *TimeSeries) nextSample() *Sample {
	if len(m.Samples) < cap(m.Samples) {
		m.Samples = m.Samples[:len(m.Samples)+1]
		if m.Samples[len(m.Samples)-1] == nil {
			m.Samples[len(m.Samples)-1] = &Sample{}
		}
		return m.Samples[len(m.Samples)-1]
	}

	s := &Sample{}
	m.Samples = append(m.Samples, s)
	return s
}

func (m *TimeSeries) AppendLabel(key []byte, value []byte) {
	l := m.nextLabel()
	l.Name = append(l.Name[:0], key...)
	l.Value = append(l.Value[:0], value...)
}

func (m *TimeSeries) AppendLabelString(key string, value string) {
	l := m.nextLabel()
	l.Name = append(l.Name[:0], key...)
	l.Value = append(l.Value[:0], value...)
}

func (m *TimeSeries) AppendSample(timestamp int64, value float64) {
	s := m.nextSample()
	s.Timestamp = timestamp
	s.Value = value
}

func (m *Label) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.Name)
	if l > 0 {
		n += 1 + l + sizeOf(uint64(l))
	}
	l = len(m.Value)
	if l > 0 {
		n += 1 + l + sizeOf(uint64(l))
	}
	return n
}

func (m *Label) marshalProtobuf(mm *easyproto.MessageMarshaler) {
	mm.AppendBytes(1, m.Name)
	mm.AppendBytes(2, m.Value)
}

func (m *Label) unmarshalProtobuf(src []byte) (err error) {
	// Set default Sample values
	m.Name = m.Name[:0]
	m.Value = m.Value[:0]

	// Parse Sample message at src
	var fc easyproto.FieldContext
	for len(src) > 0 {
		src, err = fc.NextField(src)
		if err != nil {
			return fmt.Errorf("cannot read next field in sample")
		}
		switch fc.FieldNum {
		case 1:
			name, ok := fc.Bytes()
			if !ok {
				return fmt.Errorf("cannot read sample value")
			}
			m.Name = append(m.Name[:0], name...)
		case 2:
			value, ok := fc.Bytes()
			if !ok {
				return fmt.Errorf("cannot read sample value")
			}
			m.Value = append(m.Value[:0], value...)
		}
	}
	return nil

}

func (l *Label) Reset() {
	if cap(l.Name) > maxRetainedLabelBytes {
		l.Name = nil
	} else {
		l.Name = l.Name[:0]
	}
	if cap(l.Value) > maxRetainedLabelBytes {
		l.Value = nil
	} else {
		l.Value = l.Value[:0]
	}
}

func sizeOf(x uint64) (n int) {
	return (mathbits.Len64(x|1) + 6) / 7
}
