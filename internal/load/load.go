package load

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Averages holds the 1m, 5m, and 15m load averages.
type Averages struct {
	Load1m  float64
	Load5m  float64
	Load15m float64
}

// GetCPUCount returns the number of logical CPUs usable by the current process.
func GetCPUCount() int {
	return runtime.NumCPU()
}

// ReadLoadAvgFunc is a function variable to allow mocking in tests.
// It holds the implementation for reading load averages.
var ReadLoadAvgFunc = func() (*Averages, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/loadavg: %w", err)
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid format in /proc/loadavg: expected at least 3 fields, got %d", len(fields))
	}

	load1m, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse 1m load average: %w", err)
	}

	load5m, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse 5m load average: %w", err)
	}

	load15m, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse 15m load average: %w", err)
	}

	return &Averages{
		Load1m:  load1m,
		Load5m:  load5m,
		Load15m: load15m,
	}, nil
}

// ReadLoadAvg calls ReadLoadAvgFunc, allowing ReadLoadAvgFunc to be replaced for testing.
func ReadLoadAvg() (*Averages, error) {
	return ReadLoadAvgFunc()
}

// NormalizeLoadAverages divides the load averages by the number of CPU cores.
func NormalizeLoadAverages(avg *Averages, cpuCount int) *Averages {
	if cpuCount <= 0 {
		// Avoid division by zero or negative. Should not happen in practice.
		// If it does, return raw averages.
		return avg
	}
	return &Averages{
		Load1m:  avg.Load1m / float64(cpuCount),
		Load5m:  avg.Load5m / float64(cpuCount),
		Load15m: avg.Load15m / float64(cpuCount),
	}
}
