package wal

// Destination identifies a normalized storage table, independently of its schema.
type Destination struct {
	Database string
	Table    string
}

func (d Destination) String() string { return d.Database + "." + d.Table }

func DestinationFromFilename(path string) (Destination, error) {
	db, table, _, _, err := ParseFilename(path)
	return Destination{Database: db, Table: table}, err
}

func DestinationFromPrefix(prefix string) (Destination, error) {
	return DestinationFromFilename(prefix + "_0.wal")
}
