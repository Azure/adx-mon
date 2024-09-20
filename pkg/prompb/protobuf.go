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
	LabelPool = NewPool[*Label](
		func() *Label {
			return &Label{}
		},
	)

	SamplePool = NewPool[*Sample](
		func() *Sample {
			return &Sample{}
		},
	)
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
		ts := wr.Timeseries[i]
		TimeSeriesPool.Put(ts)
	}
	wr.Timeseries = wr.Timeseries[:0]
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
			if cap(m.Labels) > len(m.Labels) {
				m.Labels = m.Labels[:len(m.Labels)+1]
				m.Labels[len(m.Labels)-1] = LabelPool.Get()
			} else {
				m.Labels = append(m.Labels, LabelPool.Get())
			}
			s := m.Labels[len(m.Labels)-1]
			if err := s.unmarshalProtobuf(data); err != nil {
				return fmt.Errorf("cannot unmarshal sample: %w", err)
			}
		case 2:
			data, ok := fc.MessageData()
			if !ok {
				return fmt.Errorf("cannot read Timeseries sample data")
			}
			if cap(m.Samples) > len(m.Samples) {
				m.Samples = m.Samples[:len(m.Samples)+1]
				m.Samples[len(m.Samples)-1] = SamplePool.Get()
			} else {
				m.Samples = append(m.Samples, SamplePool.Get())
			}
			s := m.Samples[len(m.Samples)-1]
			if err := s.unmarshalProtobuf(data); err != nil {
				return fmt.Errorf("cannot unmarshal sample: %w", err)
			}
		}
	}
	return nil
}

func (ts *TimeSeries) Reset() {
	for i := range ts.Labels {
		l := ts.Labels[i]
		LabelPool.Put(l)
	}
	for i := range ts.Samples {
		s := ts.Samples[i]
		SamplePool.Put(s)
	}
	ts.Labels = ts.Labels[:0]
	ts.Samples = ts.Samples[:0]
}

func (m *TimeSeries) AppendLabel(key []byte, value []byte) {
	if cap(m.Labels) > len(m.Labels) {
		m.Labels = m.Labels[:len(m.Labels)+1]
		m.Labels[len(m.Labels)-1] = LabelPool.Get()
	} else {
		m.Labels = append(m.Labels, LabelPool.Get())
	}
	l := m.Labels[len(m.Labels)-1]
	l.Name = append(l.Name[:0], key...)
	l.Value = append(l.Value[:0], value...)
}

func (m *TimeSeries) AppendLabelString(key string, value string) {
	if cap(m.Labels) > len(m.Labels) {
		m.Labels = m.Labels[:len(m.Labels)+1]
		m.Labels[len(m.Labels)-1] = LabelPool.Get()
	} else {
		m.Labels = append(m.Labels, LabelPool.Get())
	}
	l := m.Labels[len(m.Labels)-1]
	l.Name = append(l.Name[:0], key...)
	l.Value = append(l.Value[:0], value...)
}

func (m *TimeSeries) AppendSample(timestamp int64, value float64) {
	if cap(m.Samples) > len(m.Samples) {
		m.Samples = m.Samples[:len(m.Samples)+1]
		m.Samples[len(m.Samples)-1] = SamplePool.Get()
	} else {
		m.Samples = append(m.Samples, SamplePool.Get())
	}
	s := m.Samples[len(m.Samples)-1]
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
	m.Name = nil
	m.Value = nil

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
			m.Name = name
		case 2:
			value, ok := fc.Bytes()
			if !ok {
				return fmt.Errorf("cannot read sample value")
			}
			m.Value = value
		}
	}
	return nil

}

func (l *Label) Reset() {
	l.Name = l.Name[:0]
	l.Value = l.Value[:0]
}

func sizeOf(x uint64) (n int) {
	return (mathbits.Len64(x|1) + 6) / 7
}
