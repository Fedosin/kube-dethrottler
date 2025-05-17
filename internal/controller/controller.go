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
	"github.com/Fedosin/kube-dethrottler/internal/load"
)

// Controller manages the main loop of reading load, checking thresholds, and tainting.
type Controller struct {
	config        *config.Config
	kubeClient    kubernetes.KubeClientInterface
	cpuCount      int
	tainted       bool
	lastTaintTime time.Time
	logger        *log.Logger
}

// NewController creates a new Controller instance.
func NewController(cfg *config.Config, kubeClient kubernetes.KubeClientInterface, logger *log.Logger) *Controller {
	return &Controller{
		config:     cfg,
		kubeClient: kubeClient,
		cpuCount:   load.GetCPUCount(),
		tainted:    false, // Assume not tainted initially, will verify
		logger:     logger,
	}
}

// Run starts the main loop of the controller.
func (c *Controller) Run(ctx context.Context) {
	c.logger.Printf("Starting kube-dethrottler on node: %s", c.config.NodeName)
	c.logger.Printf("CPU Cores: %d", c.cpuCount)
	c.logger.Printf("Poll Interval: %s", c.config.PollInterval)
	c.logger.Printf("Cooldown Period: %s", c.config.CooldownPeriod)
	c.logger.Printf("Taint Key: %s, Effect: %s", c.config.TaintKey, c.config.TaintEffect)
	c.logger.Printf("Thresholds: Load1m: %.2f, Load5m: %.2f, Load15m: %.2f (0 means disabled)",
		c.config.Thresholds.Load1m, c.config.Thresholds.Load5m, c.config.Thresholds.Load15m)

	if c.config.NodeName == "" {
		c.logger.Fatal("Node name is not configured. Ensure NODE_NAME env var is set via Downward API or in config.")
	}

	// Initial check for existing taint
	existingTaint, err := c.kubeClient.HasTaint(ctx, c.config.NodeName, c.config.TaintKey, c.config.TaintEffect)
	if err != nil {
		c.logger.Printf("Error checking initial taint status: %v. Assuming not tainted.", err)
		c.tainted = false
	} else {
		c.tainted = existingTaint
		if c.tainted {
			c.lastTaintTime = time.Now() // If already tainted, assume it was just now for cooldown purposes
			c.logger.Printf("Node is already tainted with %s=%s:%s", c.config.TaintKey, "high-load", c.config.TaintEffect)
		}
	}

	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Println("Shutting down controller...")
			// Attempt to remove taint on shutdown if it was applied by this controller
			// This is a best-effort, context might be already cancelled.
			// Consider a separate context for this cleanup with a short timeout.
			if c.tainted {
				c.logger.Printf("Attempting to remove taint %s on shutdown...", c.config.TaintKey)
				err := c.kubeClient.RemoveTaint(context.Background(), c.config.NodeName, c.config.TaintKey, c.config.TaintEffect)
				if err != nil {
					c.logger.Printf("Failed to remove taint on shutdown: %v", err)
				} else {
					c.logger.Printf("Taint %s removed successfully on shutdown.", c.config.TaintKey)
				}
			}
			return
		case <-ticker.C:
			c.checkLoadAndTaint(ctx)
		}
	}
}

func (c *Controller) checkLoadAndTaint(ctx context.Context) {
	rawAverages, err := load.ReadLoadAvg()
	if err != nil {
		c.logger.Printf("Error reading load averages: %v", err)
		return
	}

	normalizedAverages := load.NormalizeLoadAverages(rawAverages, c.cpuCount)
	c.logger.Printf("Raw Load: 1m=%.2f, 5m=%.2f, 15m=%.2f", rawAverages.Load1m, rawAverages.Load5m, rawAverages.Load15m)
	c.logger.Printf("Normalized Load: 1m=%.2f, 5m=%.2f, 15m=%.2f", normalizedAverages.Load1m, normalizedAverages.Load5m, normalizedAverages.Load15m)

	exceeded := false
	if c.config.Thresholds.Load1m > 0 && normalizedAverages.Load1m > c.config.Thresholds.Load1m {
		c.logger.Printf("Load1m (%.2f) exceeded threshold (%.2f)", normalizedAverages.Load1m, c.config.Thresholds.Load1m)
		exceeded = true
	}
	if c.config.Thresholds.Load5m > 0 && normalizedAverages.Load5m > c.config.Thresholds.Load5m {
		c.logger.Printf("Load5m (%.2f) exceeded threshold (%.2f)", normalizedAverages.Load5m, c.config.Thresholds.Load5m)
		exceeded = true
	}
	if c.config.Thresholds.Load15m > 0 && normalizedAverages.Load15m > c.config.Thresholds.Load15m {
		c.logger.Printf("Load15m (%.2f) exceeded threshold (%.2f)", normalizedAverages.Load15m, c.config.Thresholds.Load15m)
		exceeded = true
	}

	if exceeded {
		if !c.tainted {
			c.logger.Printf("Threshold exceeded. Applying taint %s=%s:%s to node %s",
				c.config.TaintKey, "high-load", c.config.TaintEffect, c.config.NodeName)
			err := c.kubeClient.ApplyTaint(ctx, c.config.NodeName, c.config.TaintKey, "high-load", c.config.TaintEffect)
			if err != nil {
				c.logger.Printf("Error applying taint: %v", err)
			} else {
				c.tainted = true
				c.lastTaintTime = time.Now()
				c.logger.Printf("Taint %s applied successfully.", c.config.TaintKey)
			}
		} else {
			c.logger.Printf("Threshold exceeded, but node is already tainted. Updating lastTaintTime for cooldown.")
			c.lastTaintTime = time.Now() // Update timestamp to prolong cooldown if still exceeding
		}
	} else {
		if c.tainted {
			if time.Since(c.lastTaintTime) >= c.config.CooldownPeriod {
				c.logger.Printf("All metrics below thresholds and cooldown period (%s) passed. Removing taint %s from node %s",
					c.config.CooldownPeriod, c.config.TaintKey, c.config.NodeName)
				err := c.kubeClient.RemoveTaint(ctx, c.config.NodeName, c.config.TaintKey, c.config.TaintEffect)
				if err != nil {
					c.logger.Printf("Error removing taint: %v", err)
				} else {
					c.tainted = false
					c.logger.Printf("Taint %s removed successfully.", c.config.TaintKey)
				}
			} else {
				c.logger.Printf("Metrics are below thresholds, but cooldown period (%s) not yet passed. Time since last taint: %s",
					c.config.CooldownPeriod, time.Since(c.lastTaintTime))
			}
		} else {
			c.logger.Print("All metrics below thresholds. No action needed.")
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
