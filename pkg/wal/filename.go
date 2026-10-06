package wal

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	ErrNotWALSegment     = errors.New("not a WAL segment")
	ErrInvalidWALSegment = errors.New("invalid WAL segment")
)

func ParseFilename(path string) (database, table, schema, epoch string, err error) {
	if filepath.Ext(path) != ".wal" {
		err = ErrNotWALSegment
		return
	}

	fileName := filepath.Base(path)
	fields := strings.Split(fileName, "_")
	if len(fields) < 3 {
		err = ErrInvalidWALSegment
		return
	}

	if len(fields) == 3 {
		database = fields[0]
		table = fields[1]
		epoch = fields[2][:len(fields[2])-4]
		schema = ""

		if database == "" || table == "" || epoch == "" {
			err = ErrInvalidWALSegment
		}
		return
	} else if len(fields) == 4 {
		database = fields[0]
		table = fields[1]
		schema = fields[2]
		epoch = fields[3][:len(fields[3])-4]

		if database == "" || table == "" || schema == "" || epoch == "" {
			err = ErrInvalidWALSegment
		}

		return
	}
	err = ErrInvalidWALSegment
	return
}

// ParsePrefix returns the database and table of a WAL prefix in the form <database>_<table> or
// <database>_<table>_<schema>.
func ParsePrefix(prefix string) (database, table string, err error) {
	database, rest, ok := strings.Cut(prefix, "_")
	if !ok || database == "" {
		return "", "", ErrInvalidWALSegment
	}
	table, schema, hasSchema := strings.Cut(rest, "_")
	if table == "" || (hasSchema && (schema == "" || strings.Contains(schema, "_"))) {
		return "", "", ErrInvalidWALSegment
	}
	return database, table, nil
}

func Filename(database, table, schema, epoch string) string {
	if schema == "" {
		return fmt.Sprintf("%s_%s_%s.wal", database, table, epoch)
	}
	return fmt.Sprintf("%s_%s_%s_%s.wal", database, table, schema, epoch)
}

type File struct {
	Path     string
	Database string
	Table    string
	Schema   string
	Epoch    string
	Key      string
}
