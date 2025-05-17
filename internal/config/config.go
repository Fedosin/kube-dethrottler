package config

import (
	"os"
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
	NodeName       string        `yaml:"nodeName"` // Injected via Downward API
	PollInterval   time.Duration `yaml:"pollInterval"`
	CooldownPeriod time.Duration `yaml:"cooldownPeriod"`
	TaintKey       string        `yaml:"taintKey"`
	TaintEffect    string        `yaml:"taintEffect"` // e.g., "NoSchedule", "PreferNoSchedule", "NoExecute"
	Thresholds     Thresholds    `yaml:"thresholds"`
	KubeconfigPath string        `yaml:"kubeconfigPath"` // Optional: for local development
	ConfigFilePath string        `yaml:"configFilePath"` // Path to this config file, for reloading or context
}

// LoadConfig reads the YAML configuration file and returns a Config struct.
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
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
	cfg.ConfigFilePath = configPath // Store the path for reference

	// NodeName will be typically set via downward API in a K8s environment
	// but can be overridden in config for local testing.
	if cfg.NodeName == "" {
		// Attempt to get node name from environment if not in config (common for downward API)
		cfg.NodeName = os.Getenv("NODE_NAME")
	}

	return &cfg, nil
}
