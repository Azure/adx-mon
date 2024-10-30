package journal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/cespare/xxhash"
)

type journalcursor struct {
	Cursor string `json:"cursor"`
}

func cursorPath(cursorDirectory string, filters []string, database string, table string) string {
	hasher := xxhash.New()
	// filters are order dependent, so do not sort before creating the hash.
	for _, filter := range filters {
		hasher.Write([]byte(filter))
	}
	filterHash := hasher.Sum64()
	fileName := fmt.Sprintf("journal_%s_%s_%x.cursor", database, table, filterHash)
	sanitized := strings.ReplaceAll(fileName, string(filepath.Separator), "_")
	return filepath.Join(cursorDirectory, sanitized)
}

func writeCursor(cursorFilePath string, cursor string) {
	if cursor == "" || cursorFilePath == "" {
		return
	}

	cursorVal := fmt.Sprintf(`{"cursor":"%s"}`, cursor)
	output, err := os.Create(cursorFilePath)
	if err != nil {
		logger.Errorf("journal: failed to create cursor file: %v", err)
		return
	}
	defer output.Close()

	_, err = output.Write([]byte(cursorVal))
	if err != nil {
		logger.Errorf("journal: failed to write cursor file: %v", err)
	}
}

func readCursor(cursorFilePath string) (string, error) {
	input, err := os.Open(cursorFilePath)
	if err != nil {
		return "", fmt.Errorf("journal: failed to open cursor file: %w", err)
	}
	defer input.Close()

	var cursor journalcursor
	if err := json.NewDecoder(input).Decode(&cursor); err != nil {
		return "", fmt.Errorf("journal: failed to decode cursor file: %w", err)
	}

	return cursor.Cursor, nil
}

func cleanCursor(cursorFilePath string) {
	if err := os.Remove(cursorFilePath); err != nil {
		logger.Errorf("journal: failed to remove cursor file: %v", err)
	}
}
