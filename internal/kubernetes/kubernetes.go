package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeClientInterface defines the methods our controller needs to interact with Kubernetes.
// This helps in mocking the client for tests.
type KubeClientInterface interface {
	ApplyTaint(ctx context.Context, nodeName, taintKey, taintValue, taintEffect string) error
	RemoveTaint(ctx context.Context, nodeName, taintKey, taintEffect string) error
	HasTaint(ctx context.Context, nodeName, taintKey, taintEffect string) (bool, error)
}

// Client provides methods to interact with the Kubernetes API.
// It implements KubeClientInterface.
type Client struct {
	clientset kubernetes.Interface
}

// Ensure Client implements KubeClientInterface
var _ KubeClientInterface = (*Client)(nil)

// NewClient creates a new Kubernetes API client.
// It tries to use in-cluster config first, then falls back to kubeconfigPath if provided.
func NewClient(kubeconfigPath string) (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Not in cluster, try kubeconfig
		if kubeconfigPath != "" {
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
			}
		} else {
			return nil, fmt.Errorf("in-cluster config failed and no kubeconfig path provided: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}
	return &Client{clientset: clientset}, nil
}

// ApplyTaint applies a taint to the specified node.
func (c *Client) ApplyTaint(ctx context.Context, nodeName, taintKey, taintValue, taintEffect string) error {
	taint := corev1.Taint{
		Key:    taintKey,
		Value:  taintValue,
		Effect: corev1.TaintEffect(taintEffect),
	}

	// Commenting out unused patch variable to resolve linter error.
	// patch := []struct {
	// 	Op    string           `json:"op"`
	// 	Path  string           `json:"path"`
	// 	Value []*corev1.Taint `json:"value,omitempty"`
	// }{
	// 	{
	// 		Op:    "add", // This will add or update existing
	// 		Path:  "/spec/taints",
	// 		Value: []*corev1.Taint{&taint},
	// 	},
	// }

	// Get current taints to avoid duplicates and to handle the patch correctly.
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	newTaints := make([]corev1.Taint, 0)
	taintExists := false
	for _, existingTaint := range node.Spec.Taints {
		if existingTaint.Key == taint.Key && existingTaint.Effect == taint.Effect {
			// If a taint with the same key and effect exists, we update its value.
			// However, the problem states "apply/remove a taint", implying one specific taint.
			// For simplicity and to match the typical use case of this controller,
			// we'll assume we're managing a single, specific taint. If it's already there
			// with the correct value, this operation is idempotent. If the value differs,
			// it would be an update. However, strategic merge patch might be better for taints.
			// Using JSON patch with "add" to `/spec/taints` can lead to duplicates if not careful.
			// A safer way is to fetch, modify, and update, or use a more specific patch.

			// Let's ensure we don't add a duplicate by replacing if found.
			newTaints = append(newTaints, taint) // Replace with the new/updated taint
			taintExists = true
		} else {
			newTaints = append(newTaints, existingTaint)
		}
	}

	if !taintExists {
		newTaints = append(newTaints, taint)
	}

	patchPayload := []struct {
		Op    string         `json:"op"`
		Path  string         `json:"path"`
		Value []corev1.Taint `json:"value"`
	}{
		{
			Op:    "replace",
			Path:  "/spec/taints",
			Value: newTaints,
		},
	}
	if len(newTaints) == 0 && len(node.Spec.Taints) > 0 { // if all taints were removed
		patchPayload[0].Value = []corev1.Taint{} // Ensure an empty array to remove all taints if newTaints is empty
	} else if len(newTaints) > 0 && len(node.Spec.Taints) == 0 { // if taints were added to a node with no taints
		patchPayload[0].Op = "add"
	}

	patchBytes, err := json.Marshal(patchPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal taint patch: %w", err)
	}

	_, err = c.clientset.CoreV1().Nodes().Patch(ctx, nodeName, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to apply taint to node %s: %w", nodeName, err)
	}
	return nil
}

// RemoveTaint removes a specific taint from the node.
func (c *Client) RemoveTaint(ctx context.Context, nodeName, taintKey, taintEffect string) error {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	var newTaints []corev1.Taint
	taintFound := false
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey && taint.Effect == corev1.TaintEffect(taintEffect) {
			taintFound = true
			continue // Skip this taint, effectively removing it
		}
		newTaints = append(newTaints, taint)
	}

	if !taintFound {
		return nil // Taint not present, nothing to do
	}

	// To remove taints, we patch the taints array with the filtered list.
	// If newTaints is empty, it means all taints (or the specific one) were removed.
	patchPayload := []struct {
		Op    string         `json:"op"`
		Path  string         `json:"path"`
		Value []corev1.Taint `json:"value"` // Use []corev1.Taint here, not []*corev1.Taint
	}{
		{
			Op:    "replace",
			Path:  "/spec/taints",
			Value: newTaints, // If newTaints is empty, this will remove all taints if it was the only one.
		},
	}
	if len(newTaints) == 0 && len(node.Spec.Taints) > 0 { // if all taints were removed
		patchPayload[0].Value = []corev1.Taint{} // Ensure an empty array to remove all taints if newTaints is empty
	}

	patchBytes, err := json.Marshal(patchPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal taint removal patch: %w", err)
	}

	_, err = c.clientset.CoreV1().Nodes().Patch(ctx, nodeName, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to remove taint from node %s: %w", nodeName, err)
	}
	return nil
}

// HasTaint checks if the node has a specific taint.
func (c *Client) HasTaint(ctx context.Context, nodeName, taintKey, taintEffect string) (bool, error) {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey && taint.Effect == corev1.TaintEffect(taintEffect) {
			return true, nil
		}
	}
	return false, nil
}
