package prompb

import (
	"fmt"

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

// WriteRequest represents Prometheus remote write API request
type WriteRequest struct {
	Timeseries []*TimeSeries

	// CommonLabels is an immutable sorted set of labels shared by every time
	// series in this request. It is materialized into each series when the
	// request is marshaled to the Prometheus remote write protobuf format.
	CommonLabels []*Label
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
	wr.CommonLabels = nil
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
	return wr.MarshalTo(nil)
}

func (wr *WriteRequest) MarshalTo(dst []byte) ([]byte, error) {
	marshaller := mp.Get()
	marshaller.Reset()
	mm := marshaller.MessageMarshaler()
	for _, ts := range wr.Timeseries {
		ts.marshalProtobuf(mm.AppendMessage(1), wr.CommonLabels)
	}
	dst = marshaller.Marshal(dst[:0])
	mp.Put(marshaller)
	return dst, nil
}

// Reset resets wr.
func (wr *WriteRequest) Reset() {
	for i := range wr.Timeseries {
		ts := wr.Timeseries[i]
		TimeSeriesPool.Put(ts)
	}
	wr.Timeseries = wr.Timeseries[:0]
	wr.CommonLabels = nil
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

func (m *TimeSeries) marshalProtobuf(mm *easyproto.MessageMarshaler, commonLabels []*Label) {
	for l := range MergedLabels(m.Labels, commonLabels) {
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
			m.Labels = append(m.Labels, &Label{})
			s := m.Labels[len(m.Labels)-1]
			if err := s.unmarshalProtobuf(data); err != nil {
				return fmt.Errorf("cannot unmarshal sample: %w", err)
			}
		case 2:
			data, ok := fc.MessageData()
			if !ok {
				return fmt.Errorf("cannot read Timeseries sample data")
			}
			m.Samples = append(m.Samples, &Sample{})
			s := m.Samples[len(m.Samples)-1]
			if err := s.unmarshalProtobuf(data); err != nil {
				return fmt.Errorf("cannot unmarshal sample: %w", err)
			}
		}
	}
	Sort(m.Labels)
	return nil
}

func (ts *TimeSeries) Reset() {
	clear(ts.Labels[:cap(ts.Labels)])
	clear(ts.Samples[:cap(ts.Samples)])
	ts.Labels = ts.Labels[:0]
	ts.Samples = ts.Samples[:0]
}

func (m *TimeSeries) AppendLabel(key []byte, value []byte) {
	m.Labels = append(m.Labels, &Label{})
	l := m.Labels[len(m.Labels)-1]
	l.Name = append(l.Name[:0], key...)
	l.Value = append(l.Value[:0], value...)
}

func (m *TimeSeries) AppendLabelString(key string, value string) {
	m.Labels = append(m.Labels, &Label{})
	l := m.Labels[len(m.Labels)-1]
	l.Name = append(l.Name[:0], key...)
	l.Value = append(l.Value[:0], value...)
}

func (m *TimeSeries) AppendSample(timestamp int64, value float64) {
	m.Samples = append(m.Samples, &Sample{})
	s := m.Samples[len(m.Samples)-1]
	s.Timestamp = timestamp
	s.Value = value
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
	l.Name = l.Name[:0]
	l.Value = l.Value[:0]
}
