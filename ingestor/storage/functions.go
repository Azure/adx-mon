package storage

type StorageFunction struct {
	Name string
	Body string
}

func PromMetricsFunctions() []StorageFunction {
	// functions is the list of functions that we need to create in the database.  They are executed in order.
	functions := []StorageFunction{
		{
			Name: "prom_increase",
			Body: `.create-or-alter function prom_increase (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
		T
		| where isnan(Value)==false
		| extend h=SeriesId
		| partition hint.strategy=shuffle by h (
			as Series
			| order by h, Timestamp asc
			| extend prevVal=prev(Value)
			| extend diff=Value-prevVal
			| extend Value=case(h == prev(h), case(diff < 0, next(Value)-Value, diff), real(0))
			| project-away prevVal, diff, h
		)}`},

		{
			Name: "prom_rate",
			Body: `.create-or-alter function prom_rate (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
		T
		| invoke prom_increase(interval=interval)
		| extend Value=Value/((Timestamp-prev(Timestamp))/1s)
		| where isnotnull(Value)
		| where isnan(Value) == false}`},

		{

			Name: "prom_delta",
			Body: `.create-or-alter function prom_delta (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
		T
		| where isnan(Value)==false
		| extend h=SeriesId
		| partition hint.strategy=shuffle by h (
			as Series
			| order by h, Timestamp asc
			| extend prevVal=prev(Value)
			| extend diff=Value-prevVal
			| extend Value=case(h == prev(h), case(diff < 0, next(Value)-Value, diff), real(0))
			| project-away prevVal, diff, h
		)}`},
		{

			Name: "CountCardinality",
			Body: `.create-or-alter function CountCardinality () {
				union withsource=table *
				| where Timestamp >= ago(1h) and Timestamp < ago(5m)
				| extend SeriesId=hash_xxhash64(table)
				| summarize Value=toreal(dcount(SeriesId)) by table
				| extend Timestamp=bin(now(), 1m)
				| extend Labels=bag_pack_columns(table)
				| project Timestamp, SeriesId, Labels, Value
		}`},
	}
	return functions
}

func OTLPLogsFunctions() []StorageFunction {
	return nil
}
