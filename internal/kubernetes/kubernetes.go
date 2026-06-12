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
type KubeClientInterface interface {
	ApplyTaint(ctx context.Context, nodeName, taintKey, taintValue, taintEffect string) error
	RemoveTaint(ctx context.Context, nodeName, taintKey, taintEffect string) error
	HasTaint(ctx context.Context, nodeName, taintKey, taintEffect string) (bool, error)
	ListNodeNames(ctx context.Context, labelSelector string) ([]string, error)
}

// Client provides methods to interact with the Kubernetes API.
type Client struct {
	clientset kubernetes.Interface
}

var _ KubeClientInterface = (*Client)(nil)

// NewClient creates a new Kubernetes API client.
func NewClient(kubeconfigPath string) (*Client, error) {
	cfg, err := buildConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}
	return &Client{clientset: clientset}, nil
}

// Clientset returns the underlying kubernetes.Interface for use by other
// components (e.g., PSI fetcher, leader election).
func (c *Client) Clientset() kubernetes.Interface {
	return c.clientset
}

// BuildConfig creates a rest.Config, exported for use in leader election setup.
func BuildConfig(kubeconfigPath string) (*rest.Config, error) {
	return buildConfig(kubeconfigPath)
}

func buildConfig(kubeconfigPath string) (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		if kubeconfigPath != "" {
			cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
			}
		} else {
			return nil, fmt.Errorf("in-cluster config failed and no kubeconfig path provided: %w", err)
		}
	}
	return cfg, nil
}

// ListNodeNames returns the names of all nodes matching the given label selector.
func (c *Client) ListNodeNames(ctx context.Context, labelSelector string) ([]string, error) {
	opts := metav1.ListOptions{}
	if labelSelector != "" {
		opts.LabelSelector = labelSelector
	}

	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	names := make([]string, 0, len(nodes.Items))
	for i := range nodes.Items {
		names = append(names, nodes.Items[i].Name)
	}
	return names, nil
}

// ApplyTaint adds a taint to a node.
func (c *Client) ApplyTaint(ctx context.Context, nodeName, taintKey, taintValue, effect string) error {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get node: %v", err)
	}

	taints := node.Spec.Taints
	for _, taint := range taints {
		if taint.Key == taintKey {
			return nil
		}
	}

	taints = append(taints, corev1.Taint{
		Key:    taintKey,
		Value:  taintValue,
		Effect: corev1.TaintEffect(effect),
	})
	log.Printf("Adding taint '%s' to node: %v", taintKey, nodeName)

	return updateNodeWithTaints(ctx, c.clientset, nodeName, taints)
}

// RemoveTaint removes a taint from a node.
func (c *Client) RemoveTaint(ctx context.Context, nodeName, taintKey, taintEffect string) error {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get node: %v", err)
	}

	taintFound := false
	newTaints := make([]corev1.Taint, 0, len(node.Spec.Taints))
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey && taint.Effect == corev1.TaintEffect(taintEffect) {
			taintFound = true
			continue
		}
		newTaints = append(newTaints, taint)
	}

	if !taintFound {
		return nil
	}

	log.Printf("Removing taint '%s' from node: %v", taintKey, nodeName)
	return updateNodeWithTaints(ctx, c.clientset, nodeName, newTaints)
}

func updateNodeWithTaints(ctx context.Context, clientset kubernetes.Interface, nodeName string, taints []corev1.Taint) error {
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get node: %v", err)
	}

	updatedNode := node.DeepCopy()
	updatedNode.Spec.Taints = taints

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
