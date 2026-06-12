package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")

	configData := []byte(`thresholds:
  cpu:
    some:
      avg10: 25.0
`)
	if err := os.WriteFile(configFile, configData, 0o600); err != nil {
		t.Fatalf("Failed to write temp config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, wantErr false", err)
	}

	if cfg.PollInterval != 30*time.Second {
		t.Errorf("cfg.PollInterval = %v, want %v", cfg.PollInterval, 30*time.Second)
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
	if cfg.LeaderElection.LeaseName != "kube-dethrottler-leader" {
		t.Errorf("cfg.LeaderElection.LeaseName = %v, want %v", cfg.LeaderElection.LeaseName, "kube-dethrottler-leader")
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "custom_config.yaml")

	configData := []byte(`pollInterval: "15s"
cooldownPeriod: "10m"
taintKey: "custom/taint"
taintEffect: "PreferNoSchedule"
nodeFilter: "node-role.kubernetes.io/worker"
kubeconfigPath: "/tmp/kubeconfig"
leaderElection:
  enabled: true
  leaseName: "custom-lease"
  leaseNamespace: "default"
thresholds:
  cpu:
    some:
      avg10: 30.0
      avg60: 20.0
    full:
      avg10: 15.0
  memory:
    some:
      avg10: 25.0
  io:
    full:
      avg300: 10.0
`)
	if err := os.WriteFile(configFile, configData, 0o600); err != nil {
		t.Fatalf("Failed to write temp config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, wantErr false", err)
	}

	if cfg.PollInterval != 15*time.Second {
		t.Errorf("cfg.PollInterval = %v, want %v", cfg.PollInterval, 15*time.Second)
	}
	if cfg.CooldownPeriod != 10*time.Minute {
		t.Errorf("cfg.CooldownPeriod = %v, want %v", cfg.CooldownPeriod, 10*time.Minute)
	}
	if cfg.TaintKey != "custom/taint" {
		t.Errorf("cfg.TaintKey = %v, want %v", cfg.TaintKey, "custom/taint")
	}
	if cfg.TaintEffect != "PreferNoSchedule" {
		t.Errorf("cfg.TaintEffect = %v, want %v", cfg.TaintEffect, "PreferNoSchedule")
	}
	if cfg.NodeFilter != "node-role.kubernetes.io/worker" {
		t.Errorf("cfg.NodeFilter = %v, want %v", cfg.NodeFilter, "node-role.kubernetes.io/worker")
	}
	if cfg.KubeconfigPath != "/tmp/kubeconfig" {
		t.Errorf("cfg.KubeconfigPath = %v, want %v", cfg.KubeconfigPath, "/tmp/kubeconfig")
	}
	if cfg.LeaderElection.Enabled != true {
		t.Error("cfg.LeaderElection.Enabled should be true")
	}
	if cfg.LeaderElection.LeaseName != "custom-lease" {
		t.Errorf("cfg.LeaderElection.LeaseName = %v, want %v", cfg.LeaderElection.LeaseName, "custom-lease")
	}
	if cfg.Thresholds.CPU.Some.Avg10 != 30.0 {
		t.Errorf("cfg.Thresholds.CPU.Some.Avg10 = %v, want %v", cfg.Thresholds.CPU.Some.Avg10, 30.0)
	}
	if cfg.Thresholds.CPU.Some.Avg60 != 20.0 {
		t.Errorf("cfg.Thresholds.CPU.Some.Avg60 = %v, want %v", cfg.Thresholds.CPU.Some.Avg60, 20.0)
	}
	if cfg.Thresholds.CPU.Full.Avg10 != 15.0 {
		t.Errorf("cfg.Thresholds.CPU.Full.Avg10 = %v, want %v", cfg.Thresholds.CPU.Full.Avg10, 15.0)
	}
	if cfg.Thresholds.Memory.Some.Avg10 != 25.0 {
		t.Errorf("cfg.Thresholds.Memory.Some.Avg10 = %v, want %v", cfg.Thresholds.Memory.Some.Avg10, 25.0)
	}
	if cfg.Thresholds.IO.Full.Avg300 != 10.0 {
		t.Errorf("cfg.Thresholds.IO.Full.Avg300 = %v, want %v", cfg.Thresholds.IO.Full.Avg300, 10.0)
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
	if err := os.WriteFile(configFile, invalidYAML, 0o600); err != nil {
		t.Fatalf("Failed to write temp invalid config file: %v", err)
	}

	_, err := LoadConfig(configFile)
	if err == nil {
		t.Error("LoadConfig() error = nil, wantErr true for invalid YAML")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				PollInterval:   30 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintKey:       "test/taint",
				TaintEffect:    "NoSchedule",
				Thresholds: PSIThresholds{
					CPU: PSIPressure{Some: PSIAverages{Avg10: 25.0}},
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
				Thresholds:     PSIThresholds{CPU: PSIPressure{Some: PSIAverages{Avg10: 10.0}}},
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
				Thresholds:     PSIThresholds{CPU: PSIPressure{Some: PSIAverages{Avg10: 10.0}}},
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
				Thresholds:     PSIThresholds{CPU: PSIPressure{Some: PSIAverages{Avg10: 10.0}}},
			},
			wantErr: true,
			errMsg:  "cooldownPeriod",
		},
		{
			name: "invalid taint effect",
			config: Config{
				PollInterval:   30 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintEffect:    "InvalidEffect",
				Thresholds:     PSIThresholds{CPU: PSIPressure{Some: PSIAverages{Avg10: 10.0}}},
			},
			wantErr: true,
			errMsg:  "invalid taintEffect",
		},
		{
			name: "threshold above 100",
			config: Config{
				PollInterval:   30 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintEffect:    "NoSchedule",
				Thresholds:     PSIThresholds{CPU: PSIPressure{Some: PSIAverages{Avg10: 150.0}}},
			},
			wantErr: true,
			errMsg:  "must be between 0 and 100",
		},
		{
			name: "negative threshold",
			config: Config{
				PollInterval:   30 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintEffect:    "NoSchedule",
				Thresholds:     PSIThresholds{Memory: PSIPressure{Full: PSIAverages{Avg10: -5.0}}},
			},
			wantErr: true,
			errMsg:  "must be between 0 and 100",
		},
		{
			name: "all thresholds disabled",
			config: Config{
				PollInterval:   30 * time.Second,
				CooldownPeriod: 5 * time.Minute,
				TaintEffect:    "NoSchedule",
				Thresholds:     PSIThresholds{},
			},
			wantErr: true,
			errMsg:  "at least one PSI threshold must be set",
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
