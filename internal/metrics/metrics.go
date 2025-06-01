package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// LoadAverage1m tracks the 1-minute normalized load average.
	LoadAverage1m = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kube_dethrottler_load_average_1m",
		Help: "The 1-minute normalized load average (load/cpu_cores)",
	}, []string{"node"})

	// LoadAverage5m tracks the 5-minute normalized load average.
	LoadAverage5m = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kube_dethrottler_load_average_5m",
		Help: "The 5-minute normalized load average (load/cpu_cores)",
	}, []string{"node"})

	// LoadAverage15m tracks the 15-minute normalized load average.
	LoadAverage15m = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kube_dethrottler_load_average_15m",
		Help: "The 15-minute normalized load average (load/cpu_cores)",
	}, []string{"node"})

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
	}, []string{"node", "metric"})
)
