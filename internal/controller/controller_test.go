package controller

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/Fedosin/kube-dethrottler/internal/config"
	"github.com/Fedosin/kube-dethrottler/internal/kubernetes"
	"github.com/Fedosin/kube-dethrottler/internal/load"
	corev1 "k8s.io/api/core/v1"
)

// mockKubeClient implements kubernetes.KubeClientInterface for controller tests
type mockKubeClient struct {
	taints          map[string]corev1.Taint
	hasTaintErr     error
	applyTaintErr   error
	removeTaintErr  error
	taintApplied    bool // To check if ApplyTaint was called
	taintRemoved    bool // To check if RemoveTaint was called
	appliedTaintKey string
	removedTaintKey string
}

func newMockKubeClient() *mockKubeClient {
	return &mockKubeClient{
		taints: make(map[string]corev1.Taint),
	}
}

func (m *mockKubeClient) HasTaint(ctx context.Context, nodeName, taintKey, taintEffect string) (bool, error) {
	if m.hasTaintErr != nil {
		return false, m.hasTaintErr
	}
	key := taintKey + "-" + taintEffect
	_, exists := m.taints[key]
	return exists, nil
}

func (m *mockKubeClient) ApplyTaint(ctx context.Context, nodeName, taintKey, taintValue, taintEffect string) error {
	m.taintApplied = true
	m.taintRemoved = false
	m.appliedTaintKey = taintKey
	if m.applyTaintErr != nil {
		return m.applyTaintErr
	}
	key := taintKey + "-" + taintEffect
	m.taints[key] = corev1.Taint{Key: taintKey, Value: taintValue, Effect: corev1.TaintEffect(taintEffect)}
	return nil
}

func (m *mockKubeClient) RemoveTaint(ctx context.Context, nodeName, taintKey, taintEffect string) error {
	m.taintRemoved = true
	m.taintApplied = false
	m.removedTaintKey = taintKey
	if m.removeTaintErr != nil {
		return m.removeTaintErr
	}
	key := taintKey + "-" + taintEffect
	delete(m.taints, key)
	return nil
}

// Ensure mockKubeClient implements the interface
var _ kubernetes.KubeClientInterface = (*mockKubeClient)(nil)

// Global vars for mocking load values within the test file scope
var currentMockLoadAverages *load.Averages
var currentMockReadLoadAvgError error

// Original load.ReadLoadAvg function, to be restored after tests that modify it.
// This direct monkey-patching is generally discouraged but used here for simplicity in test setup.
// A better approach involves interfaces for dependencies or function vars within the `load` package.
var originalReadLoadAvg func() (*load.Averages, error)

func setupMockLoadReader(avg *load.Averages, err error) {
	originalReadLoadAvg = load.ReadLoadAvgFunc // Store original
	currentMockLoadAverages = avg
	currentMockReadLoadAvgError = err
	load.ReadLoadAvgFunc = func() (*load.Averages, error) { // Patch
		return currentMockLoadAverages, currentMockReadLoadAvgError
	}
}

func teardownMockLoadReader() {
	load.ReadLoadAvgFunc = originalReadLoadAvg // Restore
}

func newTestController(cfg *config.Config, mockClient kubernetes.KubeClientInterface, logger *log.Logger) *Controller {
	return &Controller{
		config:     cfg,
		kubeClient: mockClient,
		cpuCount:   1, // Default to 1 for predictable normalized load in tests
		tainted:    false,
		logger:     logger,
	}
}

func TestNewController(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := &config.Config{NodeName: "test-node"}
	mockClient := newMockKubeClient()

	// We pass the mockClient directly as it satisfies the KubeClientInterface
	ctrl := NewController(cfg, mockClient, logger)

	if ctrl.config.NodeName != "test-node" {
		t.Errorf("Expected controller node name to be %s, got %s", "test-node", ctrl.config.NodeName)
	}
	// cpuCount is initialized by load.GetCPUCount() in NewController, so we check against that.
	if ctrl.cpuCount != load.GetCPUCount() {
		t.Errorf("Expected CPU count to be %d, got %d", load.GetCPUCount(), ctrl.cpuCount)
	}
}

func TestController_Run_InitialTaintCheck_NodeAlreadyTainted(t *testing.T) {
	logger := log.New(os.Stdout, "test-run-init: ", log.LstdFlags)
	cfg := &config.Config{
		NodeName:       "test-node-init",
		PollInterval:   10 * time.Millisecond, // Short poll for quick test
		TaintKey:       "init-taint",
		TaintEffect:    "NoSchedule",
		CooldownPeriod: 1 * time.Minute,
	}
	mockKube := newMockKubeClient()
	// Simulate node already having the taint
	mockKube.taints[cfg.TaintKey+"-"+cfg.TaintEffect] = corev1.Taint{Key: cfg.TaintKey, Value: "high-load", Effect: corev1.TaintEffect(cfg.TaintEffect)}

	ctrl := newTestController(cfg, mockKube, logger)
	// cpuCount will be set by load.GetCPUCount() in NewController call above.
	// Forcing it to 1 here for predictable normalized load in this specific test run for checkLoadAndTaint.
	ctrl.cpuCount = 1

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond) // Run for a short time
	defer cancel()

	setupMockLoadReader(&load.Averages{Load1m: 0.1, Load5m: 0.1, Load15m: 0.1}, nil) // Low load
	defer teardownMockLoadReader()

	go ctrl.Run(ctx)
	time.Sleep(20 * time.Millisecond) // Allow Run to perform initial check

	if !ctrl.tainted {
		t.Error("Controller should be in tainted state after recognizing an existing taint")
	}
	if ctrl.lastTaintTime.IsZero() {
		t.Error("lastTaintTime should be set when an existing taint is recognized")
	}

	// Verify no new ApplyTaint was called if it already existed
	if mockKube.taintApplied {
		t.Error("Expected ApplyTaint to not be called on mock client")
	}
}

func TestController_checkLoadAndTaint_ApplyTaint(t *testing.T) {
	logger := log.New(os.Stdout, "test-apply: ", log.LstdFlags)
	cfg := &config.Config{
		NodeName:       "test-node-apply",
		TaintKey:       "app-specific-taint",
		TaintEffect:    "NoExecute",
		Thresholds:     config.Thresholds{Load1m: 0.5},
		CooldownPeriod: 1 * time.Minute,
	}
	mockKube := newMockKubeClient()
	ctrl := newTestController(cfg, mockKube, logger)
	ctrl.cpuCount = 1 // Force CPU count for predictable normalized load

	setupMockLoadReader(&load.Averages{Load1m: 1.0, Load5m: 0.8, Load15m: 0.6}, nil) // High load
	defer teardownMockLoadReader()

	ctrl.checkLoadAndTaint(context.Background())

	if !mockKube.taintApplied {
		t.Error("Expected ApplyTaint to be called on mock client")
	}
	if mockKube.appliedTaintKey != cfg.TaintKey {
		t.Errorf("Expected taint key %s to be applied, got %s", cfg.TaintKey, mockKube.appliedTaintKey)
	}
	if !ctrl.tainted {
		t.Error("Controller state 'tainted' should be true after applying taint")
	}
	keyInMock := cfg.TaintKey + "-" + cfg.TaintEffect
	if _, exists := mockKube.taints[keyInMock]; !exists {
		t.Errorf("Taint %s not found in mock client's taints map", keyInMock)
	}
}

func TestController_checkLoadAndTaint_RemoveTaint(t *testing.T) {
	logger := log.New(os.Stdout, "test-remove: ", log.LstdFlags)
	cfg := &config.Config{
		NodeName:       "test-node-remove",
		TaintKey:       "remove-this-taint",
		TaintEffect:    "NoSchedule",
		Thresholds:     config.Thresholds{Load1m: 1.0, Load5m: 1.0, Load15m: 1.0},
		CooldownPeriod: 1 * time.Millisecond, // Very short cooldown
	}
	mockKube := newMockKubeClient()
	ctrl := newTestController(cfg, mockKube, logger)
	ctrl.cpuCount = 1

	// Setup initial state: tainted and cooldown passed
	ctrl.tainted = true
	ctrl.lastTaintTime = time.Now().Add(-1 * time.Minute) // Cooldown clearly passed
	taintKeyInMap := cfg.TaintKey + "-" + cfg.TaintEffect
	mockKube.taints[taintKeyInMap] = corev1.Taint{Key: cfg.TaintKey, Effect: corev1.TaintEffect(cfg.TaintEffect)}

	setupMockLoadReader(&load.Averages{Load1m: 0.1, Load5m: 0.1, Load15m: 0.1}, nil) // Low load
	defer teardownMockLoadReader()

	ctrl.checkLoadAndTaint(context.Background())

	if !mockKube.taintRemoved {
		t.Error("Expected RemoveTaint to be called on mock client")
	}
	if mockKube.removedTaintKey != cfg.TaintKey {
		t.Errorf("Expected taint key %s to be removed, got %s", cfg.TaintKey, mockKube.removedTaintKey)
	}
	if ctrl.tainted {
		t.Error("Controller state 'tainted' should be false after removing taint")
	}
	if _, exists := mockKube.taints[taintKeyInMap]; exists {
		t.Errorf("Taint %s still exists in mock client's taints map after removal", taintKeyInMap)
	}
}

func TestController_checkLoadAndTaint_StillInCooldown(t *testing.T) {
	logger := log.New(os.Stdout, "test-cooldown: ", log.LstdFlags)
	cfg := &config.Config{
		NodeName:       "test-node-cooldown",
		TaintKey:       "cooldown-test-taint",
		TaintEffect:    "PreferNoSchedule",
		Thresholds:     config.Thresholds{Load1m: 1.0},
		CooldownPeriod: 5 * time.Minute, // Standard cooldown
	}
	mockKube := newMockKubeClient()
	ctrl := newTestController(cfg, mockKube, logger)
	ctrl.cpuCount = 1

	// Setup initial state: tainted, but cooldown NOT passed
	ctrl.tainted = true
	ctrl.lastTaintTime = time.Now().Add(-1 * time.Minute) // Only 1 min into 5 min cooldown
	taintKeyInMap := cfg.TaintKey + "-" + cfg.TaintEffect
	mockKube.taints[taintKeyInMap] = corev1.Taint{Key: cfg.TaintKey, Effect: corev1.TaintEffect(cfg.TaintEffect)}

	setupMockLoadReader(&load.Averages{Load1m: 0.1, Load5m: 0.1, Load15m: 0.1}, nil) // Low load
	defer teardownMockLoadReader()

	ctrl.checkLoadAndTaint(context.Background())

	if mockKube.taintRemoved {
		t.Error("RemoveTaint should NOT have been called as node is in cooldown")
	}
	if !ctrl.tainted {
		t.Error("Controller state 'tainted' should remain true during cooldown")
	}
	if _, exists := mockKube.taints[taintKeyInMap]; !exists {
		t.Errorf("Taint %s was unexpectedly removed from mock client during cooldown", taintKeyInMap)
	}
}

func TestController_Run_ShutdownRemovesTaint(t *testing.T) {
	logger := log.New(os.Stdout, "test-shutdown: ", log.LstdFlags)
	cfg := &config.Config{
		NodeName:       "test-node-shutdown",
		PollInterval:   20 * time.Millisecond, // Fast poll
		TaintKey:       "shutdown-taint-key",
		TaintEffect:    "NoSchedule",
		Thresholds:     config.Thresholds{Load1m: 0.5}, // Ensure it would taint if not for shutdown
		CooldownPeriod: 1 * time.Minute,
	}
	mockKube := newMockKubeClient()
	ctrl := newTestController(cfg, mockKube, logger)
	ctrl.cpuCount = 1

	// Start the controller as if it has just tainted the node
	ctrl.tainted = true
	ctrl.lastTaintTime = time.Now()
	mockKube.taints[cfg.TaintKey+"-"+cfg.TaintEffect] = corev1.Taint{Key: cfg.TaintKey, Value: "high-load", Effect: corev1.TaintEffect(cfg.TaintEffect)}

	ctx, cancel := context.WithCancel(context.Background())

	setupMockLoadReader(&load.Averages{Load1m: 0.1, Load5m: 0.1, Load15m: 0.1}, nil) // Low load
	defer teardownMockLoadReader()

	go ctrl.Run(ctx)

	time.Sleep(5 * time.Millisecond)  // Allow Run to start, possibly one quick tick
	cancel()                          // Trigger shutdown
	time.Sleep(50 * time.Millisecond) // Give ample time for shutdown goroutine in Run to complete RemoveTaint

	if !mockKube.taintRemoved {
		t.Errorf("Expected RemoveTaint to be called on shutdown. Taint removed: %v, Taint key: %s", mockKube.taintRemoved, mockKube.removedTaintKey)
	}
	if mockKube.removedTaintKey != cfg.TaintKey {
		t.Errorf("Expected taint key %s to be removed on shutdown, got %s", cfg.TaintKey, mockKube.removedTaintKey)
	}
}
