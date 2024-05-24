package types

const (
	// Body map keys
	// BodyKeyMessage is the key for the unparsed message field of a log.
	BodyKeyMessage = "message"

	// Attributes map keys
	// AttributeDatabaseName is the name of the ADX database that the log should be sent to.
	AttributeDatabaseName = "adxmon_destination_database"
	// AttributeTableName is the name of the ADX table that the log should be sent to.
	AttributeTableName = "adxmon_destination_table"
)
