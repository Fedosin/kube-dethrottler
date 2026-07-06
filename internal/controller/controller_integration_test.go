package controller

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"
	"time"

	"github.com/Fedosin/kube-dethrottler/internal/config"
	"github.com/Fedosin/kube-dethrottler/internal/psi"
	corev1 "k8s.io/api/core/v1"
)

type mockPSIFetcher struct {
	results map[string]*psi.NodePSI
	err     error
}

func (m *mockPSIFetcher) FetchNodePSI(_ context.Context, nodeName string) (*psi.NodePSI, error) {
	if m.err != nil {
		return nil, m.err
	}
	result, ok := m.results[nodeName]
	if !ok {
		return nil, errors.New("node not found")
	}
	return result, nil
}

func TestController_PollAllNodes_AppliesTaintOnHighLoad(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1", "node-2"})
	mockPSI := &mockPSIFetcher{
		results: map[string]*psi.NodePSI{
			"node-1": {CPU: psi.Pressure{Some: psi.Averages{Avg10: 50.0}}},
			"node-2": {CPU: psi.Pressure{Some: psi.Averages{Avg10: 5.0}}},
		},
	}

	ctrl := newControllerWithMockPSI(cfg, mockKube, mockPSI, logger)
	ctrl.pollAllNodes(context.Background())

	if !mockKube.hasTaintForNode("node-1", cfg.TaintKey, cfg.TaintEffect) {
		t.Error("Expected taint on node-1 (high load)")
	}
	if mockKube.hasTaintForNode("node-2", cfg.TaintKey, cfg.TaintEffect) {
		t.Error("Expected no taint on node-2 (low load)")
	}
}

func TestController_PollAllNodes_RemovesStaleNodeState(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1"})
	mockPSI := &mockPSIFetcher{
		results: map[string]*psi.NodePSI{
			"node-1": {CPU: psi.Pressure{Some: psi.Averages{Avg10: 5.0}}},
		},
	}

	ctrl := newControllerWithMockPSI(cfg, mockKube, mockPSI, logger)
	ctrl.nodes["removed-node"] = &nodeState{tainted: false}
	ctrl.nodes["node-1"] = &nodeState{tainted: false}

	ctrl.pollAllNodes(context.Background())

	if _, exists := ctrl.nodes["removed-node"]; exists {
		t.Error("Expected removed-node state to be cleaned up")
	}
	if _, exists := ctrl.nodes["node-1"]; !exists {
		t.Error("Expected node-1 state to remain")
	}
}

func TestController_PollAllNodes_ListNodesError(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient(nil)
	mockKube.listNodesErr = errors.New("api server unavailable")
	mockPSI := &mockPSIFetcher{}

	ctrl := newControllerWithMockPSI(cfg, mockKube, mockPSI, logger)
	ctrl.nodes["node-1"] = &nodeState{tainted: true}

	ctrl.pollAllNodes(context.Background())

	if _, exists := ctrl.nodes["node-1"]; !exists {
		t.Error("Expected node state to be preserved on list error")
	}
}

func TestController_CheckNode_PSIFetchError(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1"})
	mockPSI := &mockPSIFetcher{err: errors.New("connection refused")}

	ctrl := newControllerWithMockPSI(cfg, mockKube, mockPSI, logger)
	ctrl.nodes["node-1"] = &nodeState{tainted: false}

	ctrl.checkNode(context.Background(), "node-1")

	if mockKube.getApplyCalls() != 0 {
		t.Error("Expected no taint changes when PSI fetch fails")
	}
}

func TestController_CheckNode_DetectsExistingTaint(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1"})
	mockKube.taints["node-1/"+cfg.TaintKey+"-"+cfg.TaintEffect] = corev1.Taint{}
	mockPSI := &mockPSIFetcher{
		results: map[string]*psi.NodePSI{
			"node-1": {CPU: psi.Pressure{Some: psi.Averages{Avg10: 5.0}}},
		},
	}

	ctrl := newControllerWithMockPSI(cfg, mockKube, mockPSI, logger)

	ctrl.checkNode(context.Background(), "node-1")

	state := ctrl.nodes["node-1"]
	if state == nil {
		t.Fatal("Expected node state to be created")
	}
	if !state.tainted {
		t.Error("Expected node state to reflect existing taint")
	}
}

func TestController_CheckNode_HasTaintError(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1"})
	mockKube.hasTaintErr = errors.New("api error")
	mockPSI := &mockPSIFetcher{
		results: map[string]*psi.NodePSI{
			"node-1": {CPU: psi.Pressure{Some: psi.Averages{Avg10: 50.0}}},
		},
	}

	ctrl := newControllerWithMockPSI(cfg, mockKube, mockPSI, logger)

	ctrl.checkNode(context.Background(), "node-1")

	if _, exists := ctrl.nodes["node-1"]; exists {
		t.Error("Expected node state not to be created on HasTaint error")
	}
}

func TestController_HandleExceeded_AlreadyTainted_ResetsCooldown(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1"})

	ctrl := NewController(cfg, mockKube, psi.NewFetcher(nil), logger)
	oldTime := time.Now().Add(-10 * time.Minute)
	ctrl.nodes["node-1"] = &nodeState{tainted: true, lastTaintTime: oldTime}

	ctrl.handleExceeded(context.Background(), "node-1", ctrl.nodes["node-1"])

	if mockKube.getApplyCalls() != 0 {
		t.Error("Expected no apply call for already-tainted node")
	}
	if !ctrl.nodes["node-1"].lastTaintTime.After(oldTime) {
		t.Error("Expected lastTaintTime to be refreshed")
	}
}

func TestController_HandleExceeded_ApplyError(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()

	mockKube := newMockKubeClient([]string{"node-1"})
	mockKube.applyTaintErr = errors.New("conflict")

	ctrl := NewController(cfg, mockKube, psi.NewFetcher(nil), logger)
	ctrl.nodes["node-1"] = &nodeState{tainted: false}

	ctrl.handleExceeded(context.Background(), "node-1", ctrl.nodes["node-1"])

	if ctrl.nodes["node-1"].tainted {
		t.Error("Expected node to remain untainted when apply fails")
	}
}

func TestController_HandleNotExceeded_RemoveError(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()
	cfg.CooldownPeriod = 1 * time.Millisecond

	mockKube := newMockKubeClient([]string{"node-1"})
	mockKube.removeTaintErr = errors.New("conflict")

	ctrl := NewController(cfg, mockKube, psi.NewFetcher(nil), logger)
	ctrl.nodes["node-1"] = &nodeState{tainted: true, lastTaintTime: time.Now().Add(-1 * time.Minute)}

	ctrl.handleNotExceeded(context.Background(), "node-1", ctrl.nodes["node-1"])

	if !ctrl.nodes["node-1"].tainted {
		t.Error("Expected node to remain tainted when remove fails")
	}
}

func TestController_Run_ContextCancellation(t *testing.T) {
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)
	cfg := testConfig()
	cfg.PollInterval = 10 * time.Millisecond

	mockKube := newMockKubeClient([]string{"node-1"})
	mockKube.taints["node-1/"+cfg.TaintKey+"-"+cfg.TaintEffect] = corev1.Taint{}
	mockPSI := &mockPSIFetcher{
		results: map[string]*psi.NodePSI{
			"node-1": {CPU: psi.Pressure{Some: psi.Averages{Avg10: 50.0}}},
		},
	}

	ctrl := newControllerWithMockPSI(cfg, mockKube, mockPSI, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		ctrl.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Controller did not shut down after context cancellation")
	}
}

// newControllerWithMockPSI creates a Controller with a mock PSI fetcher
// by embedding it in the struct. This requires the psiFetchFunc field.
func newControllerWithMockPSI(cfg *config.Config, kubeClient *mockKubeClient, mockPSI *mockPSIFetcher, logger *log.Logger) *Controller {
	ctrl := NewController(cfg, kubeClient, psi.NewFetcher(nil), logger)
	ctrl.psiFetchFunc = mockPSI.FetchNodePSI
	return ctrl
}
