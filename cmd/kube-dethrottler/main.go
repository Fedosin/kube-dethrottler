package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/Fedosin/kube-dethrottler/internal/config"
	"github.com/Fedosin/kube-dethrottler/internal/controller"
	"github.com/Fedosin/kube-dethrottler/internal/kubernetes"
)

func main() {
	logger := log.New(os.Stdout, "kube-dethrottler: ", log.LstdFlags|log.Lshortfile)

	configFile := flag.String("config", "/etc/kube-dethrottler/config.yaml", "Path to the configuration file.")
	flag.Parse()

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		logger.Fatalf("Failed to load configuration from %s: %v", *configFile, err)
	}

	kubeClient, err := kubernetes.NewClient(cfg.KubeconfigPath) // KubeconfigPath can be empty for in-cluster
	if err != nil {
		logger.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl := controller.NewController(cfg, kubeClient, logger)

	// Start signal watcher for graceful shutdown
	controller.WatchSignals(cancel, logger)

	// Run the controller
	ctrl.Run(ctx)

	logger.Println("kube-dethrottler has shut down.")
}
