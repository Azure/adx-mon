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

	seq := num & 0xFFFF

	num = num >> (flake.HostBits + flake.SequenceBits)
	createdAt := flake.Epoch.Add(time.Duration(num) * time.Millisecond).Add(time.Duration(seq) * time.Nanosecond)

	return createdAt, nil
}
