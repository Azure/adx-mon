package flake

import (
	"strconv"
	"time"

	"github.com/davidnarayan/go-flake"
)

func ParseFlakeID(id string) (time.Time, error) {
	num, err := strconv.ParseInt(id, 16, 64)
	if err != nil {
		return time.Time{}, err
	}
	num = num >> (flake.HostBits + flake.SequenceBits)
	createdAt := flake.Epoch.Add(time.Duration(num) * time.Millisecond)

	return createdAt, nil
}
