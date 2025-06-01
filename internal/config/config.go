package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Thresholds defines the load average limits.
// A value of 0 for any threshold disables the check for that specific period.
type Thresholds struct {
	Load1m  float64 `yaml:"load1m"`
	Load5m  float64 `yaml:"load5m"`
	Load15m float64 `yaml:"load15m"`
}

// Config holds the application configuration.
type Config struct {
	NodeName       string        `yaml:"nodeName"`
	TaintKey       string        `yaml:"taintKey"`
	TaintEffect    string        `yaml:"taintEffect"`
	KubeconfigPath string        `yaml:"kubeconfigPath"`
	ConfigFilePath string        `yaml:"configFilePath"`
	Thresholds     Thresholds    `yaml:"thresholds"`
	PollInterval   time.Duration `yaml:"pollInterval"`
	CooldownPeriod time.Duration `yaml:"cooldownPeriod"`
}

// LoadConfig reads the YAML configuration file and returns a Config struct.
func LoadConfig(configPath string) (*Config, error) {
	// Sanitize and validate the config path
	cleanPath := filepath.Clean(configPath)

	// Ensure the path is absolute
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	// Check if the file exists and is a regular file
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access config file: %w", err)
	}
	if fileInfo.IsDir() {
		return nil, fmt.Errorf("config path is a directory, not a file: %s", absPath)
	}

	// Validate file extension
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

	// Set default values if not provided
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10 * time.Second // Default poll interval
	}
	if cfg.CooldownPeriod == 0 {
		cfg.CooldownPeriod = 5 * time.Minute // Default cooldown period
	}
	if cfg.TaintKey == "" {
		cfg.TaintKey = "kube-dethrottler/high-load"
	}
	if cfg.TaintEffect == "" {
		cfg.TaintEffect = "NoSchedule" // Default TaintEffect
	}
	cfg.ConfigFilePath = absPath // Store the path for reference

	// NodeName will be typically set via downward API in a K8s environment
	// but can be overridden in config for local testing.
	if cfg.NodeName == "" {
		// Attempt to get node name from environment if not in config (common for downward API)
		cfg.NodeName = os.Getenv("NODE_NAME")
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	// Validate poll interval
	if c.PollInterval < 1*time.Second {
		return fmt.Errorf("pollInterval must be at least 1 second, got %s", c.PollInterval)
	}
	if c.PollInterval > 5*time.Minute {
		return fmt.Errorf("pollInterval should not exceed 5 minutes, got %s", c.PollInterval)
	}

	// Validate cooldown period
	if c.CooldownPeriod < c.PollInterval {
		return fmt.Errorf("cooldownPeriod (%s) must be greater than pollInterval (%s)", c.CooldownPeriod, c.PollInterval)
	}

	// Validate taint effect
	validEffects := map[string]bool{
		"NoSchedule":       true,
		"PreferNoSchedule": true,
		"NoExecute":        true,
	}
	if !validEffects[c.TaintEffect] {
		return fmt.Errorf("invalid taintEffect: %s. Must be one of: NoSchedule, PreferNoSchedule, NoExecute", c.TaintEffect)
	}

	// Validate thresholds
	if c.Thresholds.Load1m < 0 || c.Thresholds.Load5m < 0 || c.Thresholds.Load15m < 0 {
		return fmt.Errorf("load thresholds cannot be negative")
	}

	// Warn if all thresholds are disabled
	if c.Thresholds.Load1m == 0 && c.Thresholds.Load5m == 0 && c.Thresholds.Load15m == 0 {
		return fmt.Errorf("at least one load threshold must be set (non-zero)")
	}

	return nil
}
