package controller

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Fedosin/kube-dethrottler/internal/config"
	"github.com/Fedosin/kube-dethrottler/internal/kubernetes"
	"github.com/Fedosin/kube-dethrottler/internal/psi"
)

type nodeState struct {
	lastTaintTime time.Time
	tainted       bool
}

// Controller manages the main loop of fetching PSI metrics, checking thresholds,
// and tainting overloaded nodes.
type Controller struct {
	kubeClient kubernetes.KubeClientInterface
	psiFetcher *psi.Fetcher
	config     *config.Config
	logger     *log.Logger
	nodes      map[string]*nodeState
}

// NewController creates a new Controller instance.
func NewController(cfg *config.Config, kubeClient kubernetes.KubeClientInterface, psiFetcher *psi.Fetcher, logger *log.Logger) *Controller {
	return &Controller{
		config:     cfg,
		kubeClient: kubeClient,
		psiFetcher: psiFetcher,
		logger:     logger,
		nodes:      make(map[string]*nodeState),
	}
}

// Run starts the main loop of the controller.
func (c *Controller) Run(ctx context.Context) {
	c.logger.Printf("Starting kube-dethrottler (PSI mode)")
	c.logger.Printf("Poll Interval: %s", c.config.PollInterval)
	c.logger.Printf("Cooldown Period: %s", c.config.CooldownPeriod)
	c.logger.Printf("Taint Key: %s, Effect: %s", c.config.TaintKey, c.config.TaintEffect)
	if c.config.NodeFilter != "" {
		c.logger.Printf("Node Filter: %s", c.config.NodeFilter)
	}

	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	// Run immediately on startup
	c.pollAllNodes(ctx)

	for {
		select {
		case <-ctx.Done():
			c.logger.Println("Shutting down controller...")
			c.cleanupTaints()
			return
		case <-ticker.C:
			c.pollAllNodes(ctx)
		}
	}
}

func (c *Controller) cleanupTaints() {
	for nodeName, state := range c.nodes {
		if state.tainted {
			c.logger.Printf("Attempting to remove taint %s from node %s on shutdown...", c.config.TaintKey, nodeName)
			err := c.kubeClient.RemoveTaint(context.Background(), nodeName, c.config.TaintKey, c.config.TaintEffect)
			if err != nil {
				c.logger.Printf("Failed to remove taint from node %s on shutdown: %v", nodeName, err)
			} else {
				c.logger.Printf("Taint %s removed from node %s on shutdown.", c.config.TaintKey, nodeName)
			}
		}
	}
}

func (c *Controller) pollAllNodes(ctx context.Context) {
	nodeNames, err := c.kubeClient.ListNodeNames(ctx, c.config.NodeFilter)
	if err != nil {
		c.logger.Printf("Error listing nodes: %v", err)
		return
	}

	// Clean up state for nodes that no longer exist
	for name := range c.nodes {
		found := false
		for _, n := range nodeNames {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			delete(c.nodes, name)
		}
	}

	for _, nodeName := range nodeNames {
		c.checkNode(ctx, nodeName)
	}
}

func (c *Controller) checkNode(ctx context.Context, nodeName string) {
	state, exists := c.nodes[nodeName]
	if !exists {
		hasTaint, err := c.kubeClient.HasTaint(ctx, nodeName, c.config.TaintKey, c.config.TaintEffect)
		if err != nil {
			c.logger.Printf("Error checking taint on node %s: %v", nodeName, err)
			return
		}
		state = &nodeState{tainted: hasTaint}
		if hasTaint {
			state.lastTaintTime = time.Now()
			c.logger.Printf("Node %s already has taint %s", nodeName, c.config.TaintKey)
		}
		c.nodes[nodeName] = state
	}

	nodePSI, err := c.psiFetcher.FetchNodePSI(ctx, nodeName)
	if err != nil {
		c.logger.Printf("Error fetching PSI for node %s: %v", nodeName, err)
		return
	}

	exceeded := c.isThresholdExceeded(nodePSI, nodeName)

	if exceeded {
		c.handleExceeded(ctx, nodeName, state)
	} else {
		c.handleNotExceeded(ctx, nodeName, state)
	}
}

func (c *Controller) isThresholdExceeded(nodePSI *psi.NodePSI, nodeName string) bool {
	return c.checkAverages(nodePSI.CPU.Some, c.config.Thresholds.CPU.Some, nodeName, "cpu.some") ||
		c.checkAverages(nodePSI.CPU.Full, c.config.Thresholds.CPU.Full, nodeName, "cpu.full") ||
		c.checkAverages(nodePSI.Memory.Some, c.config.Thresholds.Memory.Some, nodeName, "memory.some") ||
		c.checkAverages(nodePSI.Memory.Full, c.config.Thresholds.Memory.Full, nodeName, "memory.full") ||
		c.checkAverages(nodePSI.IO.Some, c.config.Thresholds.IO.Some, nodeName, "io.some") ||
		c.checkAverages(nodePSI.IO.Full, c.config.Thresholds.IO.Full, nodeName, "io.full")
}

func (c *Controller) checkAverages(actual psi.Averages, threshold config.PSIAverages, nodeName, label string) bool {
	exceeded := false

	if threshold.Avg10 > 0 && actual.Avg10 > threshold.Avg10 {
		c.logger.Printf("Node %s: %s.avg10 (%.2f) exceeded threshold (%.2f)", nodeName, label, actual.Avg10, threshold.Avg10)
		exceeded = true
	}
	if threshold.Avg60 > 0 && actual.Avg60 > threshold.Avg60 {
		c.logger.Printf("Node %s: %s.avg60 (%.2f) exceeded threshold (%.2f)", nodeName, label, actual.Avg60, threshold.Avg60)
		exceeded = true
	}
	if threshold.Avg300 > 0 && actual.Avg300 > threshold.Avg300 {
		c.logger.Printf("Node %s: %s.avg300 (%.2f) exceeded threshold (%.2f)", nodeName, label, actual.Avg300, threshold.Avg300)
		exceeded = true
	}

	return exceeded
}

func (c *Controller) handleExceeded(ctx context.Context, nodeName string, state *nodeState) {
	if state.tainted {
		state.lastTaintTime = time.Now()
		return
	}

	c.logger.Printf("Threshold exceeded on node %s. Applying taint %s=%s:%s",
		nodeName, c.config.TaintKey, "high-load", c.config.TaintEffect)
	err := c.kubeClient.ApplyTaint(ctx, nodeName, c.config.TaintKey, "high-load", c.config.TaintEffect)
	if err != nil {
		c.logger.Printf("Error applying taint to node %s: %v", nodeName, err)
	} else {
		state.tainted = true
		state.lastTaintTime = time.Now()
		c.logger.Printf("Taint %s applied to node %s.", c.config.TaintKey, nodeName)
	}
}

func (c *Controller) handleNotExceeded(ctx context.Context, nodeName string, state *nodeState) {
	if !state.tainted {
		return
	}

	if time.Since(state.lastTaintTime) >= c.config.CooldownPeriod {
		c.logger.Printf("All metrics below thresholds on node %s and cooldown passed. Removing taint %s",
			nodeName, c.config.TaintKey)
		err := c.kubeClient.RemoveTaint(ctx, nodeName, c.config.TaintKey, c.config.TaintEffect)
		if err != nil {
			c.logger.Printf("Error removing taint from node %s: %v", nodeName, err)
		} else {
			state.tainted = false
			c.logger.Printf("Taint %s removed from node %s.", c.config.TaintKey, nodeName)
		}
	}
}

// WatchSignals sets up a listener for OS signals to gracefully shut down.
func WatchSignals(cancel context.CancelFunc, logger *log.Logger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Printf("Received signal: %s. Initiating shutdown...", sig)
		cancel()
	}()
}
