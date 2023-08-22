package wal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrNotWALSegment     = errors.New("not a WAL segment")
	ErrInvalidWALSegment = errors.New("invalid WAL segment")
)

func ParseFilename(path string) (database, table, epoch string, err error) {
	if filepath.Ext(path) != ".wal" {
		err = ErrNotWALSegment
		return
	}

	fileName := filepath.Base(path)
	fields := strings.Split(fileName, "_")
	if len(fields) != 3 {
		err = ErrInvalidWALSegment
		return
	}
	database = fields[0]
	table = fields[1]
	epoch = fields[2][:len(fields[2])-4]
	return
}

func Filename(database, table, epoch string) string {
	return fmt.Sprintf("%s_%s_%s.wal", database, table, epoch)
}

type File struct {
	Path     string
	Database string
	Table    string
	Epoch    string
	Key      string
}

func ListDir(storageDir string) ([]File, error) {
	var files []File
	filepath.WalkDir(storageDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".wal" {
			return nil
		}

		fileName := filepath.Base(path)
		fields := strings.Split(fileName, "_")
		if len(fields) != 3 {
			return nil
		}

		files = append(files, File{
			Path:     path,
			Database: fields[0],
			Table:    fields[1],
			Epoch:    fields[2][:len(fields[2])-4],
			Key:      fmt.Sprintf("%s_%s", fields[0], fields[1]),
		},
		)
		return nil
	})
	return files, nil
}
