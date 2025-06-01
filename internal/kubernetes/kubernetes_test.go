package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestNewClient_InCluster(t *testing.T) {
	// This test is hard to do without actually being in a cluster
	// or significantly mocking the environment variables and service account files
	// that rest.InClusterConfig() expects.
	// We will assume that if KubeconfigPath is empty and it doesn't panic or return an obvious error immediately,
	// it *would* try in-cluster. A full test requires an integration environment.
	t.Skip("Skipping in-cluster NewClient test; requires actual in-cluster environment or complex mocking.")
}

func TestNewClient_WithKubeconfig(t *testing.T) {
	// Create a dummy kubeconfig file
	tempFile, err := os.CreateTemp("", "kubeconfig-")
	if err != nil {
		t.Fatalf("Failed to create temp kubeconfig file: %v", err)
	}
	defer func() {
		if err := tempFile.Close(); err != nil {
			t.Errorf("Failed to close temp kubeconfig file: %v", err)
		}
		if err := os.Remove(tempFile.Name()); err != nil {
			t.Errorf("Failed to remove temp kubeconfig file: %v", err)
		}
	}()

	kubeconfigContent := `
apiVersion: v1
clusters:
- cluster:
    server: http://localhost:8080
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
kind: Config
users:
- name: test-user
`
	if _, err := tempFile.WriteString(kubeconfigContent); err != nil {
		t.Fatalf("Failed to write to temp kubeconfig file: %v", err)
	}

	client, err := NewClient(tempFile.Name())
	if err != nil {
		t.Fatalf("NewClient() with kubeconfig error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() with kubeconfig returned nil client")
	}
	if client.clientset == nil {
		t.Fatal("NewClient() with kubeconfig returned client with nil clientset")
	}
}

func TestNewClient_NoConfig(t *testing.T) {
	_, err := NewClient("") // No in-cluster, no kubeconfig path
	if err == nil {
		t.Error("NewClient() with no config, error = nil, wantErr true")
	}
}

func TestApplyTaint(t *testing.T) {
	ctx := context.Background()
	nodeName := "test-node"
	taintKey := "test-key"
	taintValue := "test-value"
	taintEffect := "NoSchedule"

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}

	client := fake.NewSimpleClientset(node)
	k8sClient := &Client{clientset: client}

	client.PrependReactor("patch", "nodes", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		patchAction := action.(clienttesting.PatchAction)
		if patchAction.GetName() != nodeName {
			return false, nil, fmt.Errorf("unexpected node name in patch: got %s, want %s", patchAction.GetName(), nodeName)
		}
		if patchAction.GetPatchType() != types.JSONPatchType {
			return false, nil, fmt.Errorf("unexpected patch type: got %s, want %s", patchAction.GetPatchType(), types.JSONPatchType)
		}

		var patches []map[string]interface{}
		if err := json.Unmarshal(patchAction.GetPatch(), &patches); err != nil {
			return false, nil, fmt.Errorf("failed to unmarshal patch: %v", err)
		}

		if len(patches) == 0 || patches[0]["op"] != "replace" && patches[0]["op"] != "add" {
			return false, nil, fmt.Errorf("expected 'replace' or 'add' op, got %v", patches[0]["op"])
		}
		if patches[0]["path"] != "/spec/taints" {
			return false, nil, fmt.Errorf("expected path '/spec/taints', got %v", patches[0]["path"])
		}

		taints, ok := patches[0]["value"].([]interface{})
		if !ok {
			return false, nil, fmt.Errorf("patch value is not a slice: %T", patches[0]["value"])
		}

		found := false
		for _, t := range taints {
			taintMap, ok := t.(map[string]interface{})
			if !ok {
				return false, nil, fmt.Errorf("taint in patch is not a map: %T", t)
			}
			if taintMap["key"] == taintKey && taintMap["value"] == taintValue && taintMap["effect"] == taintEffect {
				found = true
				break
			}
		}
		if !found {
			return false, nil, fmt.Errorf("applied taint not found in patch: key=%s, value=%s, effect=%s. Actual patch: %s", taintKey, taintValue, taintEffect, string(patchAction.GetPatch()))
		}
		// Simulate successful patch by returning the node (it's not actually modified by this fake reactor)
		return true, node, nil
	})

	err := k8sClient.ApplyTaint(ctx, nodeName, taintKey, taintValue, taintEffect)
	if err != nil {
		t.Errorf("ApplyTaint() error = %v, wantErr false", err)
	}
}

func TestRemoveTaint(t *testing.T) {
	ctx := context.Background()
	nodeName := "test-node"
	taintKeyToRemove := "key-to-remove"
	taintEffectToRemove := "NoSchedule"

	existingTaints := []corev1.Taint{
		{Key: taintKeyToRemove, Value: "val1", Effect: corev1.TaintEffect(taintEffectToRemove)},
		{Key: "other-key", Value: "val2", Effect: "NoExecute"},
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		Spec:       corev1.NodeSpec{Taints: existingTaints},
	}

	client := fake.NewSimpleClientset(node)
	k8sClient := &Client{clientset: client}

	client.PrependReactor("patch", "nodes", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		patchAction := action.(clienttesting.PatchAction)
		var patches []map[string]interface{}
		if errJson := json.Unmarshal(patchAction.GetPatch(), &patches); errJson != nil {
			return false, nil, fmt.Errorf("failed to unmarshal patch in reactor: %v", errJson)
		}

		if len(patches) == 0 || patches[0]["op"] != "replace" || patches[0]["path"] != "/spec/taints" {
			return false, nil, fmt.Errorf("unexpected patch operation or path: op=%v, path=%v", patches[0]["op"], patches[0]["path"])
		}

		patchedTaints, ok := patches[0]["value"].([]interface{})
		if !ok {
			return false, nil, fmt.Errorf("patched taints value is not a slice: %T", patches[0]["value"])
		}

		// Check that the taint to remove is NOT in the patched taints
		for _, tInf := range patchedTaints {
			taintMap, ok := tInf.(map[string]interface{})
			if !ok {
				return false, nil, fmt.Errorf("taint in patch is not a map")
			}
			if taintMap["key"] == taintKeyToRemove && taintMap["effect"] == taintEffectToRemove {
				return false, nil, fmt.Errorf("taint %s:%s was not removed by patch", taintKeyToRemove, taintEffectToRemove)
			}
		}

		// Check that other taints are preserved
		foundOtherKey := false
		for _, tInf := range patchedTaints {
			taintMap, ok := tInf.(map[string]interface{})
			if !ok {
				return false, nil, fmt.Errorf("taint in patch is not a map")
			}
			if taintMap["key"] == "other-key" {
				foundOtherKey = true
				break
			}
		}
		if !foundOtherKey && len(existingTaints) > 1 {
			return false, nil, fmt.Errorf("'other-key' was not preserved in patch. Patch: %s", string(patchAction.GetPatch()))
		}
		return true, node, nil
	})

	err := k8sClient.RemoveTaint(ctx, nodeName, taintKeyToRemove, taintEffectToRemove)
	if err != nil {
		t.Errorf("RemoveTaint() error = %v, wantErr false", err)
	}
}

func TestHasTaint(t *testing.T) {
	ctx := context.Background()
	nodeName := "test-node"
	testCases := []struct {
		taintToFind    corev1.Taint
		name           string
		existingTaints []corev1.Taint
		expected       bool
		expectError    bool
	}{
		{
			name:           "Taint exists",
			existingTaints: []corev1.Taint{{Key: "key1", Value: "val1", Effect: "NoSchedule"}},
			taintToFind:    corev1.Taint{Key: "key1", Value: "val1", Effect: "NoSchedule"},
			expected:       true,
		},
		{
			name:           "Taint does not exist (different key)",
			existingTaints: []corev1.Taint{{Key: "key1", Value: "val1", Effect: "NoSchedule"}},
			taintToFind:    corev1.Taint{Key: "key2", Value: "val1", Effect: "NoSchedule"},
			expected:       false,
		},
		{
			name:           "Taint does not exist (different effect)",
			existingTaints: []corev1.Taint{{Key: "key1", Value: "val1", Effect: "NoSchedule"}},
			taintToFind:    corev1.Taint{Key: "key1", Value: "val1", Effect: "NoExecute"},
			expected:       false,
		},
		{
			name:           "No taints on node",
			existingTaints: []corev1.Taint{},
			taintToFind:    corev1.Taint{Key: "key1", Value: "val1", Effect: "NoSchedule"},
			expected:       false,
		},
		{
			name:           "Node not found",
			existingTaints: nil, // This will cause fake client to not find the node
			taintToFind:    corev1.Taint{Key: "key1", Value: "val1", Effect: "NoSchedule"},
			expected:       false,
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var node *corev1.Node
			if tc.existingTaints != nil { // If nil, node is not added to fake client, simulating not found
				node = &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: nodeName},
					Spec:       corev1.NodeSpec{Taints: tc.existingTaints},
				}
			}

			var client *fake.Clientset
			if node != nil {
				client = fake.NewSimpleClientset(node)
			} else {
				client = fake.NewSimpleClientset() // No objects, Get will fail
			}
			k8sClient := &Client{clientset: client}

			hasTaint, err := k8sClient.HasTaint(ctx, nodeName, tc.taintToFind.Key, string(tc.taintToFind.Effect))

			if tc.expectError {
				if err == nil {
					t.Errorf("HasTaint() expected an error, but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("HasTaint() unexpected error: %v", err)
				}
				if hasTaint != tc.expected {
					t.Errorf("HasTaint() = %v, want %v", hasTaint, tc.expected)
				}
			}
		})
	}
}
