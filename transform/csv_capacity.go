package transform

// RetainedCapacity reports the capacity of the output and all payload scratch
// buffers, including those whose length was cleared by Reset. It excludes the
// fixed-size csv encoder buffer and schema/column configuration.
func (w *CSVWriter) RetainedCapacity() int {
	return w.w.Cap() + w.buf.Cap() + w.labelsBuf.Cap() + w.seriesIdBuf.Cap() + cap(w.line)
}

// RetainedCapacity reports output and payload scratch capacity (see CSVWriter).
func (w *NativeLogsCSVWriter) RetainedCapacity() int {
	return w.w.Cap() + w.buf.Cap() + w.labelsBuf.Cap() + w.seriesIdBuf.Cap() + cap(w.line)
}

// RetainedCapacity reports output and payload scratch capacity (see CSVWriter).
func (w *MetricsCSVWriter) RetainedCapacity() int {
	return w.w.Cap() + w.buf.Cap() + w.labelsBuf.Cap() + w.seriesIdBuf.Cap() + cap(w.line)
}
