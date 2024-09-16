package prompb

func MetricName(ts *TimeSeries) []byte {
	for _, l := range ts.Labels {
		if string(l.Name) == "__name__" {
			return l.Value
		}
	}
	return nil
}
