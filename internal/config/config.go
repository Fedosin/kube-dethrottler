package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// PSIAverages defines thresholds for the three PSI averaging windows.
// A value of 0 disables the check for that window.
type PSIAverages struct {
	Avg10  float64 `yaml:"avg10"`
	Avg60  float64 `yaml:"avg60"`
	Avg300 float64 `yaml:"avg300"`
}

// PSIPressure defines thresholds for "some" and "full" pressure categories.
type PSIPressure struct {
	Some PSIAverages `yaml:"some"`
	Full PSIAverages `yaml:"full"`
}

// PSIThresholds defines pressure thresholds for CPU, memory, and I/O.
type PSIThresholds struct {
	CPU    PSIPressure `yaml:"cpu"`
	Memory PSIPressure `yaml:"memory"`
	IO     PSIPressure `yaml:"io"`
}

// LeaderElection holds leader election configuration.
type LeaderElection struct {
	LeaseName      string        `yaml:"leaseName"`
	LeaseNamespace string        `yaml:"leaseNamespace"`
	LeaseDuration  time.Duration `yaml:"leaseDuration"`
	RenewDeadline  time.Duration `yaml:"renewDeadline"`
	RetryPeriod    time.Duration `yaml:"retryPeriod"`
	Enabled        bool          `yaml:"enabled"`
}

// Config holds the application configuration.
type Config struct {
	TaintKey       string         `yaml:"taintKey"`
	TaintEffect    string         `yaml:"taintEffect"`
	KubeconfigPath string         `yaml:"kubeconfigPath"`
	ConfigFilePath string         `yaml:"-"`
	NodeFilter     string         `yaml:"nodeFilter"`
	LeaderElection LeaderElection `yaml:"leaderElection"`
	PollInterval   time.Duration  `yaml:"pollInterval"`
	CooldownPeriod time.Duration  `yaml:"cooldownPeriod"`
	Thresholds     PSIThresholds  `yaml:"thresholds"`
}

// LoadConfig reads the YAML configuration file and returns a Config struct.
func LoadConfig(configPath string) (*Config, error) {
	cleanPath := filepath.Clean(configPath)

	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access config file: %w", err)
	}
	if fileInfo.IsDir() {
		return nil, fmt.Errorf("config path is a directory, not a file: %s", absPath)
	}

	if !strings.HasSuffix(strings.ToLower(absPath), ".yaml") && !strings.HasSuffix(strings.ToLower(absPath), ".yml") {
		return nil, fmt.Errorf("config file must have .yaml or .yml extension: %s", absPath)
	}

	// #nosec G304 -- Path has been validated and sanitized above
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	cfg.setDefaults()
	cfg.ConfigFilePath = absPath

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

func (c *Config) setDefaults() {
	if c.PollInterval == 0 {
		c.PollInterval = 30 * time.Second
	}
	if c.CooldownPeriod == 0 {
		c.CooldownPeriod = 5 * time.Minute
	}
	if c.TaintKey == "" {
		c.TaintKey = "kube-dethrottler/high-load"
	}
	if c.TaintEffect == "" {
		c.TaintEffect = "NoSchedule"
	}
	if c.LeaderElection.LeaseName == "" {
		c.LeaderElection.LeaseName = "kube-dethrottler-leader"
	}
	if c.LeaderElection.LeaseNamespace == "" {
		c.LeaderElection.LeaseNamespace = os.Getenv("POD_NAMESPACE")
		if c.LeaderElection.LeaseNamespace == "" {
			c.LeaderElection.LeaseNamespace = "kube-system"
		}
	}
	if c.LeaderElection.LeaseDuration == 0 {
		c.LeaderElection.LeaseDuration = 15 * time.Second
	}
	if c.LeaderElection.RenewDeadline == 0 {
		c.LeaderElection.RenewDeadline = 10 * time.Second
	}
	if c.LeaderElection.RetryPeriod == 0 {
		c.LeaderElection.RetryPeriod = 2 * time.Second
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.PollInterval < 1*time.Second {
		return fmt.Errorf("pollInterval must be at least 1 second, got %s", c.PollInterval)
	}
	if c.PollInterval > 5*time.Minute {
		return fmt.Errorf("pollInterval should not exceed 5 minutes, got %s", c.PollInterval)
	}

	if c.CooldownPeriod < c.PollInterval {
		return fmt.Errorf("cooldownPeriod (%s) must be greater than pollInterval (%s)", c.CooldownPeriod, c.PollInterval)
	}

	validEffects := map[string]bool{
		"NoSchedule":       true,
		"PreferNoSchedule": true,
		"NoExecute":        true,
	}
	if !validEffects[c.TaintEffect] {
		return fmt.Errorf("invalid taintEffect: %s. Must be one of: NoSchedule, PreferNoSchedule, NoExecute", c.TaintEffect)
	}

	if err := validatePSIAverages(c.Thresholds.CPU.Some, "cpu.some"); err != nil {
		return err
	}
	if err := validatePSIAverages(c.Thresholds.CPU.Full, "cpu.full"); err != nil {
		return err
	}
	if err := validatePSIAverages(c.Thresholds.Memory.Some, "memory.some"); err != nil {
		return err
	}
	if err := validatePSIAverages(c.Thresholds.Memory.Full, "memory.full"); err != nil {
		return err
	}
	if err := validatePSIAverages(c.Thresholds.IO.Some, "io.some"); err != nil {
		return err
	}
	if err := validatePSIAverages(c.Thresholds.IO.Full, "io.full"); err != nil {
		return err
	}

	if !c.hasAnyThreshold() {
		return fmt.Errorf("at least one PSI threshold must be set (non-zero)")
	}

	return nil
}

func validatePSIAverages(a PSIAverages, prefix string) error {
	if a.Avg10 < 0 || a.Avg10 > 100 {
		return fmt.Errorf("%s.avg10 must be between 0 and 100, got %.2f", prefix, a.Avg10)
	}
	if a.Avg60 < 0 || a.Avg60 > 100 {
		return fmt.Errorf("%s.avg60 must be between 0 and 100, got %.2f", prefix, a.Avg60)
	}
	if a.Avg300 < 0 || a.Avg300 > 100 {
		return fmt.Errorf("%s.avg300 must be between 0 and 100, got %.2f", prefix, a.Avg300)
	}
	return nil
}

func (c *Config) hasAnyThreshold() bool {
	return hasAnyAvg(c.Thresholds.CPU.Some) ||
		hasAnyAvg(c.Thresholds.CPU.Full) ||
		hasAnyAvg(c.Thresholds.Memory.Some) ||
		hasAnyAvg(c.Thresholds.Memory.Full) ||
		hasAnyAvg(c.Thresholds.IO.Some) ||
		hasAnyAvg(c.Thresholds.IO.Full)
}

func hasAnyAvg(a PSIAverages) bool {
	return a.Avg10 > 0 || a.Avg60 > 0 || a.Avg300 > 0
}
