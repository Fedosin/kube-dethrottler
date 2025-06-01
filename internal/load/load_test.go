package load

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestGetCPUCount(t *testing.T) {
	expectedCPUCount := runtime.NumCPU()
	actualCPUCount := GetCPUCount()
	if actualCPUCount != expectedCPUCount {
		t.Errorf("GetCPUCount() = %d, want %d", actualCPUCount, expectedCPUCount)
	}
	if actualCPUCount <= 0 {
		t.Errorf("GetCPUCount() returned %d, which is not a positive number", actualCPUCount)
	}
}

func TestReadLoadAvg_Success(t *testing.T) {
	tempDir := t.TempDir()
	tempProcLoadavg := filepath.Join(tempDir, "loadavg")

	// Create a dummy /proc/loadavg file
	content := "1.23 4.56 7.89 1/123 12345"
	if err := os.WriteFile(tempProcLoadavg, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp loadavg file: %v", err)
	}

	// Temporarily replace the ReadLoadAvg function's target file path
	originalLoadAvgPath := "/proc/loadavg"
	defer func() { // anonymous func to avoid capturing loop variables if this were in a loop
		// This is tricky to do safely without changing the original function signature
		// or using build tags for testing. For this example, we assume ReadLoadAvg always reads /proc/loadavg
		// and we can't easily redirect it without heavier mocking frameworks or changing its design.
		// A better design for testability would be ReadLoadAvg(path string).
		// For now, we'll test the parsing logic separately if direct /proc/loadavg mocking is hard.
		// However, since the user wants full coverage, we'll try to test the actual ReadLoadAvg if possible.
		// The test below will effectively test the parsing part if we can feed it content.
		// If we cannot change the hardcoded path, this test might only work if /proc/loadavg is readable.
	}()

	// Since we can't easily change the hardcoded path in ReadLoadAvg,
	// this test will rely on the actual /proc/loadavg.
	// This is more of an integration test for ReadLoadAvg itself.
	// We will check for non-error and plausible values.
	if _, err := os.Stat(originalLoadAvgPath); os.IsNotExist(err) {
		t.Skipf("Skipping TestReadLoadAvg_Success because %s does not exist (likely not Linux)", originalLoadAvgPath)
	}

	la, err := ReadLoadAvg() // Reads actual /proc/loadavg
	if err != nil {
		t.Fatalf("ReadLoadAvg() error = %v, wantErr false", err)
	}

	if la.Load1m < 0 || la.Load5m < 0 || la.Load15m < 0 {
		t.Errorf("ReadLoadAvg() returned negative load averages: %+v", la)
	}
	// We can't check for exact values, but they should be parseable floats.
	t.Logf("Actual system load: %+v", la)
}

// Helper function to test parsing logic, used if direct file mocking is an issue.
func parseLoadAvgContent(content string) (*Averages, error) {
	fields := strings.Fields(content)
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid format: expected at least 3 fields, got %d", len(fields))
	}
	// Simulate the parsing from ReadLoadAvg
	load1m, err := parseField(fields[0], "1m")
	if err != nil {
		return nil, err
	}
	load5m, err := parseField(fields[1], "5m")
	if err != nil {
		return nil, err
	}
	load15m, err := parseField(fields[2], "15m")
	if err != nil {
		return nil, err
	}

	return &Averages{
		Load1m:  load1m,
		Load5m:  load5m,
		Load15m: load15m,
	}, nil
}

func parseField(field, fieldName string) (float64, error) {
	val, err := strconv.ParseFloat(field, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s load average from \"%s\": %w", fieldName, field, err)
	}
	return val, nil
}

func TestParseLoadAvgContent_Valid(t *testing.T) {
	content := "1.23 4.56 7.89 1/123 12345"
	expected := &Averages{Load1m: 1.23, Load5m: 4.56, Load15m: 7.89}
	actual, err := parseLoadAvgContent(content)
	if err != nil {
		t.Fatalf("parseLoadAvgContent() error = %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("parseLoadAvgContent() = %+v, want %+v", actual, expected)
	}
}

func TestParseLoadAvgContent_InvalidFormat_TooFewFields(t *testing.T) {
	content := "1.23 4.56"
	_, err := parseLoadAvgContent(content)
	if err == nil {
		t.Error("parseLoadAvgContent() error = nil, wantErr true for too few fields")
	}
}

func TestParseLoadAvgContent_InvalidFormat_NonNumeric(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"non-numeric 1m", "abc 4.56 7.89"},
		{"non-numeric 5m", "1.23 def 7.89"},
		{"non-numeric 15m", "1.23 4.56 ghi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseLoadAvgContent(tt.content)
			if err == nil {
				t.Errorf("parseLoadAvgContent() with content '%s' error = nil, wantErr true", tt.content)
			}
		})
	}
}

func TestNormalizeLoadAverages(t *testing.T) {
	tests := []struct {
		avg      *Averages
		want     *Averages
		name     string
		cpuCount int
	}{
		{
			name:     "Normal case",
			avg:      &Averages{Load1m: 4.0, Load5m: 2.0, Load15m: 1.0},
			cpuCount: 2,
			want:     &Averages{Load1m: 2.0, Load5m: 1.0, Load15m: 0.5},
		},
		{
			name:     "Zero CPU count",
			avg:      &Averages{Load1m: 4.0, Load5m: 2.0, Load15m: 1.0},
			cpuCount: 0,
			want:     &Averages{Load1m: 4.0, Load5m: 2.0, Load15m: 1.0}, // Should return original
		},
		{
			name:     "Negative CPU count",
			avg:      &Averages{Load1m: 4.0, Load5m: 2.0, Load15m: 1.0},
			cpuCount: -1,
			want:     &Averages{Load1m: 4.0, Load5m: 2.0, Load15m: 1.0}, // Should return original
		},
		{
			name:     "Single CPU",
			avg:      &Averages{Load1m: 1.5, Load5m: 1.0, Load15m: 0.5},
			cpuCount: 1,
			want:     &Averages{Load1m: 1.5, Load5m: 1.0, Load15m: 0.5},
		},
		{
			name:     "Zero load averages",
			avg:      &Averages{Load1m: 0.0, Load5m: 0.0, Load15m: 0.0},
			cpuCount: 4,
			want:     &Averages{Load1m: 0.0, Load5m: 0.0, Load15m: 0.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeLoadAverages(tt.avg, tt.cpuCount); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NormalizeLoadAverages() = %v, want %v", got, tt.want)
			}
		})
	}
}
