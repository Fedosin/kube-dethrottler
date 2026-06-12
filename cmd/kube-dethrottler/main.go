package main

import (
	"context"
	"flag"
	"log"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/Fedosin/kube-dethrottler/internal/config"
	"github.com/Fedosin/kube-dethrottler/internal/controller"
	"github.com/Fedosin/kube-dethrottler/internal/kubernetes"
	"github.com/Fedosin/kube-dethrottler/internal/psi"
)

func main() {
	logger := log.New(os.Stdout, "kube-dethrottler: ", log.LstdFlags|log.Lshortfile)

	configFile := flag.String("config", "/etc/kube-dethrottler/config.yaml", "Path to the configuration file.")
	flag.Parse()

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		logger.Fatalf("Failed to load configuration from %s: %v", *configFile, err)
	}

	kubeClient, err := kubernetes.NewClient(cfg.KubeconfigPath)
	if err != nil {
		logger.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	psiFetcher := psi.NewFetcher(kubeClient.Clientset())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller.WatchSignals(cancel, logger)

	ctrl := controller.NewController(cfg, kubeClient, psiFetcher, logger)

	if cfg.LeaderElection.Enabled {
		runWithLeaderElection(ctx, cancel, cfg, kubeClient, ctrl, logger)
	} else {
		ctrl.Run(ctx)
	}

	logger.Println("kube-dethrottler has shut down.")
}

func runWithLeaderElection(ctx context.Context, cancel context.CancelFunc, cfg *config.Config, kubeClient *kubernetes.Client, ctrl *controller.Controller, logger *log.Logger) {
	id, err := os.Hostname()
	if err != nil {
		logger.Fatalf("Failed to get hostname for leader election identity: %v", err)
	}

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cfg.LeaderElection.LeaseName,
			Namespace: cfg.LeaderElection.LeaseNamespace,
		},
		Client: kubeClient.Clientset().CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   cfg.LeaderElection.LeaseDuration,
		RenewDeadline:   cfg.LeaderElection.RenewDeadline,
		RetryPeriod:     cfg.LeaderElection.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				logger.Println("Acquired leadership, starting controller...")
				ctrl.Run(ctx)
			},
			OnStoppedLeading: func() {
				logger.Println("Lost leadership, shutting down...")
				cancel()
			},
			OnNewLeader: func(identity string) {
				if identity == id {
					return
				}
				logger.Printf("New leader elected: %s", identity)
			},
		},
	})
}
