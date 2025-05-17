package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoadConfig_Defaults(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")

	// Create an empty config file to test defaults
	emptyConfigData := []byte("{}")
	if err := os.WriteFile(configFile, emptyConfigData, 0644); err != nil {
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
