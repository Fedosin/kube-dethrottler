package psi

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/client-go/kubernetes"
)

// Averages holds the PSI averaging windows.
type Averages struct {
	Avg10  float64 `json:"avg10"`
	Avg60  float64 `json:"avg60"`
	Avg300 float64 `json:"avg300"`
	Total  uint64  `json:"total"`
}

// Pressure holds "some" and "full" pressure data for a single resource.
type Pressure struct {
	Some Averages `json:"some"`
	Full Averages `json:"full"`
}

// NodePSI holds PSI data for all resources on a node.
type NodePSI struct {
	CPU    Pressure
	Memory Pressure
	IO     Pressure
}

// summaryResponse is the minimal structure needed to extract node-level PSI
// from the kubelet Summary API response.
type summaryResponse struct {
	Node struct {
		CPU struct {
			PSI *Pressure `json:"psi"`
		} `json:"cpu"`
		Memory struct {
			PSI *Pressure `json:"psi"`
		} `json:"memory"`
		IO struct {
			PSI *Pressure `json:"psi"`
		} `json:"io"`
	} `json:"node"`
}

// Fetcher retrieves PSI metrics from the kubelet Summary API via the
// kube-apiserver proxy.
type Fetcher struct {
	clientset kubernetes.Interface
}

// NewFetcher creates a new PSI metrics fetcher.
func NewFetcher(clientset kubernetes.Interface) *Fetcher {
	return &Fetcher{clientset: clientset}
}

// FetchNodePSI retrieves PSI metrics for a given node by calling
// /api/v1/nodes/<nodeName>/proxy/stats/summary through the kube-apiserver.
func (f *Fetcher) FetchNodePSI(ctx context.Context, nodeName string) (*NodePSI, error) {
	data, err := f.clientset.CoreV1().
		RESTClient().
		Get().
		Resource("nodes").
		Name(nodeName).
		SubResource("proxy", "stats", "summary").
		Do(ctx).
		Raw()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch summary stats for node %s: %w", nodeName, err)
	}

	var summary summaryResponse
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse summary stats for node %s: %w", nodeName, err)
	}

	result := &NodePSI{}
	if summary.Node.CPU.PSI != nil {
		result.CPU = *summary.Node.CPU.PSI
	}
	if summary.Node.Memory.PSI != nil {
		result.Memory = *summary.Node.Memory.PSI
	}
	if summary.Node.IO.PSI != nil {
		result.IO = *summary.Node.IO.PSI
	}

	return result, nil
}
