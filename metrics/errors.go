package metrics

const (
	CreateAlertNotificationError = "create_alert_notification"
	CreateQueryContextError      = "create_query_context"
	AlerterQueryInternalError    = "query_internal"
	AlerterQueryUserError        = "query_user"
	AlerterReloadRulesError      = "reload_rules"

	// Ingestor Errors
	SyncPeersError = "sync_peers"

	// Ingestor Wal Errors
	FlushWalError      = "flush_wal"
	SyncWalError       = "sync_wal"
	BatchSegmentsError = "batch_segments"
	DeleteFileError    = "delete_file"
	StatFileError      = "stat_file"
	ParseFilenameError = "parse_filename"
	OpenFileError      = "open_file"
	SizeWalError       = "size_wal"
	CloseWalError      = "close_wal"

	// Collector Errors
	CollectorScrapeError = "scrape"
)
