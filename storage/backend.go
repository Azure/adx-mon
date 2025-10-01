package storage

import (
	"fmt"
	"strings"
)

type Backend string

const (
	BackendADX        Backend = "adx"
	BackendClickHouse Backend = "clickhouse"
)

func ParseBackend(raw string) (Backend, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("storage backend cannot be empty")
	}

	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case string(BackendADX):
		return BackendADX, nil
	case string(BackendClickHouse):
		return BackendClickHouse, nil
	default:
		return "", fmt.Errorf("unknown storage backend %q", raw)
	}
}

func (b Backend) String() string {
	return string(b)
}

func (b Backend) Validate() error {
	switch b {
	case BackendADX, BackendClickHouse:
		return nil
	case "":
		return fmt.Errorf("storage backend cannot be empty")
	default:
		return fmt.Errorf("unknown storage backend %q", b)
	}
}
