package kubernetes

import (
	"context"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// ApplyTaint adds a taint to a node
func (c *Client) ApplyTaint(ctx context.Context, nodeName, taintKey, taintValue, effect string) error {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get node: %v", err)
	}

	// Check and prepare taints
	taints := node.Spec.Taints
	taintFound := false
	for _, taint := range taints {
		if taint.Key == taintKey {
			taintFound = true
			break
		}
	}

	taintAdded := false
	if !taintFound {
		taints = append(taints, corev1.Taint{
			Key:    taintKey,
			Value:  "true",
			Effect: corev1.TaintEffect(effect),
		})
		taintAdded = true
		log.Printf("Adding taint '%s' to node: %v", taintKey, nodeName)
	}

	// Only update if taint was added
	if !taintAdded {
		return nil
	}

	return updateNodeWithTaints(ctx, c.clientset, nodeName, taints)
}


// RemoveTaint removes a taint from a node
func (c *Client) RemoveTaint(ctx context.Context, nodeName, taintKey, taintEffect string) error {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get node: %v", err)
	}

	// Check and prepare taints
	taintFound := false
	var newTaints []corev1.Taint
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey && taint.Effect == corev1.TaintEffect(taintEffect) {
			taintFound = true
			continue
		}
		newTaints = append(newTaints, taint)
	}

	if taintFound {
		log.Printf("Removing taint '%s' from node: %v", taintKey, nodeName)
	}

	// Only update if taint was removed
	if !taintFound {
		return nil
	}

	return updateNodeWithTaints(ctx, c.clientset, nodeName, newTaints)
}

// updateNodeWithTaints updates node taints
func updateNodeWithTaints(ctx context.Context, clientset kubernetes.Interface, nodeName string, taints []corev1.Taint) error {
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get node: %v", err)
	}

	// Deep copy the node to modify
	updatedNode := node.DeepCopy()

	// Update taints
	updatedNode.Spec.Taints = taints

	// Use Update instead of update
	_, err = clientset.CoreV1().Nodes().Update(ctx, updatedNode, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update node: %v", err)
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
