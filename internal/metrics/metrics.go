package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// PSIPressure tracks current PSI pressure values per node.
	PSIPressure = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kube_dethrottler_psi_pressure",
		Help: "Current PSI pressure value (percentage)",
	}, []string{"node", "resource", "type", "window"})

	// NodeTainted tracks whether a node is currently tainted.
	NodeTainted = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kube_dethrottler_node_tainted",
		Help: "Whether the node is currently tainted (1 = tainted, 0 = not tainted)",
	}, []string{"node"})

	// TaintOperations tracks the number of taint/untaint operations.
	TaintOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kube_dethrottler_taint_operations_total",
		Help: "Total number of taint operations performed",
	}, []string{"node", "operation", "status"})

	// ThresholdExceeded tracks which thresholds are currently exceeded.
	ThresholdExceeded = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kube_dethrottler_threshold_exceeded",
		Help: "Whether a specific threshold is exceeded (1 = exceeded, 0 = normal)",
	}, []string{"node", "resource", "type", "window"})

	// PollErrors tracks errors during node polling.
	PollErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kube_dethrottler_poll_errors_total",
		Help: "Total number of errors during PSI polling",
	}, []string{"node", "reason"})
)
