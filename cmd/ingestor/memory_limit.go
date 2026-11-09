package main

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/Azure/adx-mon/pkg/logger"
)

// memoryLimitFraction is the share of the container's memory limit the ingestor sets as Go's memory limit when
// --memory-limit is not given, leaving room for memory the Go runtime does not manage.
const memoryLimitFraction = 0.9

// cgroupMemoryFiles hold the container's memory limit under cgroup v2 and cgroup v1.
var cgroupMemoryFiles = []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}

// cgroupMemoryLimit returns the memory limit in the first of files that exists, and false when there is none.
func cgroupMemoryLimit(files []string) (int64, bool) {
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(b))
		n, err := strconv.ParseInt(s, 10, 64)
		// cgroup v2 reports no limit as "max", and cgroup v1 as a value near the largest int64.
		if err != nil || n <= 0 || n >= 1<<62 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// memoryLimit returns the Go memory limit to set, or 0 to leave it as it is, and where it comes from: --memory-limit
// if set; otherwise GOMEMLIMIT, which the runtime has already applied; otherwise memoryLimitFraction of the
// container's memory limit, if it has one.
func memoryLimit(flag int64, env string, container func() (int64, bool)) (int64, string) {
	if flag > 0 {
		return flag, "--memory-limit"
	}
	if env != "" {
		return 0, "GOMEMLIMIT"
	}
	if n, ok := container(); ok {
		return int64(float64(n) * memoryLimitFraction), "the container's memory limit"
	}
	return 0, ""
}

// applyMemoryLimit sets Go's memory limit, a soft limit at which the garbage collector runs more often.  Without one,
// the heap grows to about twice its live size before each collection, so a flood of large transfers, each leaving
// its buffer behind as garbage, can hold several times what the intake ceiling admits.
func applyMemoryLimit(flag int64) {
	limit, source := memoryLimit(flag, os.Getenv("GOMEMLIMIT"), func() (int64, bool) { return cgroupMemoryLimit(cgroupMemoryFiles) })
	switch {
	case limit > 0:
		debug.SetMemoryLimit(limit)
		logger.Infof("Go memory limit is %d bytes, from %s", limit, source)
	case source != "":
		logger.Infof("Go memory limit is %d bytes, from %s", debug.SetMemoryLimit(-1), source)
	default:
		logger.Infof("No Go memory limit: no --memory-limit, GOMEMLIMIT or container memory limit")
	}
}
