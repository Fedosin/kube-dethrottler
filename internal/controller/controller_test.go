package controller

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Fedosin/kube-dethrottler/internal/config"
	"github.com/Fedosin/kube-dethrottler/internal/kubernetes"
	"github.com/Fedosin/kube-dethrottler/internal/psi"
	corev1 "k8s.io/api/core/v1"
)

type mockKubeClient struct {
	hasTaintErr    error
	applyTaintErr  error
	removeTaintErr error
	listNodesErr   error
	taints         map[string]corev1.Taint
	nodeNames      []string
	mu             sync.Mutex
	applyCalls     int
	removeCalls    int
}

func newMockKubeClient(nodes []string) *mockKubeClient {
	return &mockKubeClient{
		nodeNames: nodes,
		taints:    make(map[string]corev1.Taint),
	}
}

func (m *mockKubeClient) ListNodeNames(_ context.Context, _ string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listNodesErr != nil {
		return nil, m.listNodesErr
	}
	return m.nodeNames, nil
}

func (m *mockKubeClient) HasTaint(_ context.Context, nodeName, taintKey, taintEffect string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hasTaintErr != nil {
		return false, m.hasTaintErr
	}
	key := nodeName + "/" + taintKey + "-" + taintEffect
	_, exists := m.taints[key]
	return exists, nil
}

func (m *mockKubeClient) ApplyTaint(_ context.Context, nodeName, taintKey, taintValue, taintEffect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyCalls++
	if m.applyTaintErr != nil {
		return m.applyTaintErr
	}
	key := nodeName + "/" + taintKey + "-" + taintEffect
	m.taints[key] = corev1.Taint{Key: taintKey, Value: taintValue, Effect: corev1.TaintEffect(taintEffect)}
	return nil
}

func (m *mockKubeClient) RemoveTaint(_ context.Context, nodeName, taintKey, taintEffect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeCalls++
	if m.removeTaintErr != nil {
		return m.removeTaintErr
	}
	key := nodeName + "/" + taintKey + "-" + taintEffect
	delete(m.taints, key)
	return nil
}

func (m *mockKubeClient) getApplyCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applyCalls
}

func (m *mockKubeClient) getRemoveCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removeCalls
}

func (m *mockKubeClient) hasTaintForNode(nodeName, taintKey, taintEffect string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := nodeName + "/" + taintKey + "-" + taintEffect
	_, exists := m.taints[key]
	return exists
}

var _ kubernetes.KubeClientInterface = (*mockKubeClient)(nil)

func testConfig() *config.Config {
	return &config.Config{
		PollInterval:   20 * time.Millisecond,
		CooldownPeriod: 50 * time.Millisecond,
		TaintKey:       "kube-dethrottler/high-load",
		TaintEffect:    "NoSchedule",
		Thresholds: config.PSIThresholds{
			CPU: config.PSIPressure{
				Some: config.PSIAverages{Avg10: 25.0},
			},
		},
	}
}

func TestController_ApplyTaint_WhenThresholdExceeded(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1"})
	psiFetcher := psi.NewFetcher(nil) // We won't use this directly

	ctrl := NewController(cfg, mockKube, psiFetcher, logger)

	// Simulate high pressure
	ctrl.nodes["node-1"] = &nodeState{tainted: false}
	highPSI := &psi.NodePSI{
		CPU: psi.Pressure{
			Some: psi.Averages{Avg10: 50.0},
		},
	}

	exceeded := ctrl.isThresholdExceeded(highPSI, "node-1")
	if !exceeded {
		t.Fatal("Expected threshold to be exceeded")
	}

	ctrl.handleExceeded(context.Background(), "node-1", ctrl.nodes["node-1"])

	if mockKube.getApplyCalls() != 1 {
		t.Errorf("Expected 1 apply call, got %d", mockKube.getApplyCalls())
	}
	if !ctrl.nodes["node-1"].tainted {
		t.Error("Expected node state to be tainted")
	}
}

func TestController_NoTaint_WhenBelowThreshold(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1"})
	psiFetcher := psi.NewFetcher(nil)

	ctrl := NewController(cfg, mockKube, psiFetcher, logger)
	ctrl.nodes["node-1"] = &nodeState{tainted: false}

	lowPSI := &psi.NodePSI{
		CPU: psi.Pressure{
			Some: psi.Averages{Avg10: 5.0},
		},
	}

	exceeded := ctrl.isThresholdExceeded(lowPSI, "node-1")
	if exceeded {
		t.Fatal("Expected threshold not to be exceeded")
	}

	if mockKube.getApplyCalls() != 0 {
		t.Errorf("Expected 0 apply calls, got %d", mockKube.getApplyCalls())
	}
}

func TestController_RemoveTaint_AfterCooldown(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()
	cfg.CooldownPeriod = 1 * time.Millisecond

	mockKube := newMockKubeClient([]string{"node-1"})
	psiFetcher := psi.NewFetcher(nil)

	ctrl := NewController(cfg, mockKube, psiFetcher, logger)
	ctrl.nodes["node-1"] = &nodeState{
		tainted:       true,
		lastTaintTime: time.Now().Add(-1 * time.Minute),
	}

	ctrl.handleNotExceeded(context.Background(), "node-1", ctrl.nodes["node-1"])

	if mockKube.getRemoveCalls() != 1 {
		t.Errorf("Expected 1 remove call, got %d", mockKube.getRemoveCalls())
	}
	if ctrl.nodes["node-1"].tainted {
		t.Error("Expected node state to not be tainted after removal")
	}
}

func TestController_NoRemove_DuringCooldown(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()
	cfg.CooldownPeriod = 5 * time.Minute

	mockKube := newMockKubeClient([]string{"node-1"})
	psiFetcher := psi.NewFetcher(nil)

	ctrl := NewController(cfg, mockKube, psiFetcher, logger)
	ctrl.nodes["node-1"] = &nodeState{
		tainted:       true,
		lastTaintTime: time.Now(),
	}

	ctrl.handleNotExceeded(context.Background(), "node-1", ctrl.nodes["node-1"])

	if mockKube.getRemoveCalls() != 0 {
		t.Errorf("Expected 0 remove calls during cooldown, got %d", mockKube.getRemoveCalls())
	}
	if !ctrl.nodes["node-1"].tainted {
		t.Error("Expected node to remain tainted during cooldown")
	}
}

func TestController_Shutdown_RemovesTaints(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1", "node-2"})
	// Pre-populate taints
	mockKube.taints["node-1/"+cfg.TaintKey+"-"+cfg.TaintEffect] = corev1.Taint{}
	psiFetcher := psi.NewFetcher(nil)

	ctrl := NewController(cfg, mockKube, psiFetcher, logger)
	ctrl.nodes["node-1"] = &nodeState{tainted: true, lastTaintTime: time.Now()}
	ctrl.nodes["node-2"] = &nodeState{tainted: false}

	ctrl.cleanupTaints()

	if mockKube.getRemoveCalls() != 1 {
		t.Errorf("Expected 1 remove call (only node-1 was tainted), got %d", mockKube.getRemoveCalls())
	}
	if mockKube.hasTaintForNode("node-1", cfg.TaintKey, cfg.TaintEffect) {
		t.Error("Expected taint to be removed from node-1")
	}
}

func TestController_MultipleThresholds(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()
	cfg.Thresholds = config.PSIThresholds{
		CPU: config.PSIPressure{
			Some: config.PSIAverages{Avg10: 25.0},
		},
		Memory: config.PSIPressure{
			Full: config.PSIAverages{Avg10: 5.0},
		},
		IO: config.PSIPressure{
			Some: config.PSIAverages{Avg60: 30.0},
		},
	}

	mockKube := newMockKubeClient([]string{"node-1"})
	psiFetcher := psi.NewFetcher(nil)
	ctrl := NewController(cfg, mockKube, psiFetcher, logger)

	// Only memory full pressure exceeds
	nodePSI := &psi.NodePSI{
		CPU:    psi.Pressure{Some: psi.Averages{Avg10: 10.0}},
		Memory: psi.Pressure{Full: psi.Averages{Avg10: 8.0}},
		IO:     psi.Pressure{Some: psi.Averages{Avg60: 15.0}},
	}

	exceeded := ctrl.isThresholdExceeded(nodePSI, "node-1")
	if !exceeded {
		t.Error("Expected threshold to be exceeded (memory.full.avg10 > 5.0)")
	}
}
