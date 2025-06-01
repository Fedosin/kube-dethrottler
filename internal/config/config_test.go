package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoadConfig_Defaults(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")

	// Create a config file with thresholds to test defaults
	configData := []byte(`thresholds:
  load1m: 2.0
  load5m: 1.5
  load15m: 1.0`)
	if err := os.WriteFile(configFile, configData, 0644); err != nil {
		t.Fatalf("Failed to write temp config file: %v", err)
	}

	// Set NODE_NAME env var for default node name
	testNodeName := "test-node-from-env"
	originalNodeName := os.Getenv("NODE_NAME")
	err := os.Setenv("NODE_NAME", testNodeName)
	if err != nil {
		t.Fatalf("Failed to set NODE_NAME env var: %v", err)
	}
	defer func() {
		err := os.Setenv("NODE_NAME", originalNodeName)
		if err != nil {
			t.Fatalf("Failed to restore NODE_NAME env var: %v", err)
		}
	}()

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, wantErr false", err)
	}

	if cfg.NodeName != testNodeName {
		t.Errorf("cfg.NodeName = %v, want %v", cfg.NodeName, testNodeName)
	}
	if cfg.PollInterval != 10*time.Second {
		t.Errorf("cfg.PollInterval = %v, want %v", cfg.PollInterval, 10*time.Second)
	}
	if cfg.CooldownPeriod != 5*time.Minute {
		t.Errorf("cfg.CooldownPeriod = %v, want %v", cfg.CooldownPeriod, 5*time.Minute)
	}
	if cfg.TaintKey != "kube-dethrottler/high-load" {
		t.Errorf("cfg.TaintKey = %v, want %v", cfg.TaintKey, "kube-dethrottler/high-load")
	}
	if cfg.TaintEffect != "NoSchedule" {
		t.Errorf("cfg.TaintEffect = %v, want %v", cfg.TaintEffect, "NoSchedule")
	}
	if cfg.ConfigFilePath != configFile {
		t.Errorf("cfg.ConfigFilePath = %v, want %v", cfg.ConfigFilePath, configFile)
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "custom_config.yaml")

	customData := &Config{
		NodeName:       "custom-node",
		PollInterval:   30 * time.Second,
		CooldownPeriod: 10 * time.Minute,
		TaintKey:       "custom/taint",
		TaintEffect:    "PreferNoSchedule",
		Thresholds: Thresholds{
			Load1m:  1.5,
			Load5m:  1.0,
			Load15m: 0.5,
		},
		KubeconfigPath: "/tmp/kubeconfig",
	}

	yamlData, err := yaml.Marshal(customData)
	if err != nil {
		t.Fatalf("Failed to marshal custom config: %v", err)
	}

	if err := os.WriteFile(configFile, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write temp custom config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, wantErr false", err)
	}

	if cfg.NodeName != customData.NodeName {
		t.Errorf("cfg.NodeName = %v, want %v", cfg.NodeName, customData.NodeName)
	}
	if cfg.PollInterval != customData.PollInterval {
		t.Errorf("cfg.PollInterval = %v, want %v", cfg.PollInterval, customData.PollInterval)
	}
	if cfg.CooldownPeriod != customData.CooldownPeriod {
		t.Errorf("cfg.CooldownPeriod = %v, want %v", cfg.CooldownPeriod, customData.CooldownPeriod)
	}
	if cfg.TaintKey != customData.TaintKey {
		t.Errorf("cfg.TaintKey = %v, want %v", cfg.TaintKey, customData.TaintKey)
	}
	if cfg.TaintEffect != customData.TaintEffect {
		t.Errorf("cfg.TaintEffect = %v, want %v", cfg.TaintEffect, customData.TaintEffect)
	}
	if cfg.Thresholds.Load1m != customData.Thresholds.Load1m {
		t.Errorf("cfg.Thresholds.Load1m = %v, want %v", cfg.Thresholds.Load1m, customData.Thresholds.Load1m)
	}
	if cfg.Thresholds.Load5m != customData.Thresholds.Load5m {
		t.Errorf("cfg.Thresholds.Load5m = %v, want %v", cfg.Thresholds.Load5m, customData.Thresholds.Load5m)
	}
	if cfg.Thresholds.Load15m != customData.Thresholds.Load15m {
		t.Errorf("cfg.Thresholds.Load15m = %v, want %v", cfg.Thresholds.Load15m, customData.Thresholds.Load15m)
	}
	if cfg.KubeconfigPath != customData.KubeconfigPath {
		t.Errorf("cfg.KubeconfigPath = %v, want %v", cfg.KubeconfigPath, customData.KubeconfigPath)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("nonexistentconfig.yaml")
	if err == nil {
		t.Error("LoadConfig() error = nil, wantErr true for non-existent file")
	}
}

func TestLoadConfig_InvalidYaml(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "invalid.yaml")

	invalidYAML := []byte("this: is: not: valid: yaml")
	if err := os.WriteFile(configFile, invalidYAML, 0644); err != nil {
		t.Fatalf("Failed to write temp invalid config file: %v", err)
	}

	_, err := LoadConfig(configFile)
	if err == nil {
		t.Error("LoadConfig() error = nil, wantErr true for invalid YAML")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		config  Config
		name    string
		errMsg  string
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				PollInterval:   10 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintKey:       "test/taint",
				TaintEffect:    "NoSchedule",
				Thresholds: Thresholds{
					Load1m:  2.0,
					Load5m:  1.5,
					Load15m: 1.0,
				},
			},
			wantErr: false,
		},
		{
			name: "poll interval too short",
			config: Config{
				PollInterval:   500 * time.Millisecond,
				CooldownPeriod: 5 * time.Minute,
				TaintEffect:    "NoSchedule",
				Thresholds:     Thresholds{Load1m: 1.0},
			},
			wantErr: true,
			errMsg:  "pollInterval must be at least 1 second",
		},
		{
			name: "poll interval too long",
			config: Config{
				PollInterval:   10 * time.Minute,
				CooldownPeriod: 15 * time.Minute,
				TaintEffect:    "NoSchedule",
				Thresholds:     Thresholds{Load1m: 1.0},
			},
			wantErr: true,
			errMsg:  "pollInterval should not exceed 5 minutes",
		},
		{
			name: "cooldown shorter than poll interval",
			config: Config{
				PollInterval:   30 * time.Second,
				CooldownPeriod: 10 * time.Second,
				TaintEffect:    "NoSchedule",
				Thresholds:     Thresholds{Load1m: 1.0},
			},
			wantErr: true,
			errMsg:  "cooldownPeriod",
		},
		{
			name: "invalid taint effect",
			config: Config{
				PollInterval:   10 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintEffect:    "InvalidEffect",
				Thresholds:     Thresholds{Load1m: 1.0},
			},
			wantErr: true,
			errMsg:  "invalid taintEffect",
		},
		{
			name: "negative threshold",
			config: Config{
				PollInterval:   10 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintEffect:    "NoSchedule",
				Thresholds:     Thresholds{Load1m: -1.0},
			},
			wantErr: true,
			errMsg:  "load thresholds cannot be negative",
		},
		{
			name: "all thresholds disabled",
			config: Config{
				PollInterval:   10 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintEffect:    "NoSchedule",
				Thresholds:     Thresholds{Load1m: 0, Load5m: 0, Load15m: 0},
			},
			wantErr: true,
			errMsg:  "at least one load threshold must be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Config.Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}
