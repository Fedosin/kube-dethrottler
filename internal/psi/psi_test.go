package psi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func newTestClientset(t *testing.T, handler http.Handler) kubernetes.Interface {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("Failed to create clientset: %v", err)
	}
	return clientset
}

func TestFetchNodePSI_AllMetrics(t *testing.T) {
	response := map[string]any{
		"node": map[string]any{
			"cpu": map[string]any{
				"psi": map[string]any{
					"some": map[string]any{"avg10": 12.5, "avg60": 8.3, "avg300": 5.1, "total": 100000},
					"full": map[string]any{"avg10": 3.2, "avg60": 2.1, "avg300": 1.0, "total": 50000},
				},
			},
			"memory": map[string]any{
				"psi": map[string]any{
					"some": map[string]any{"avg10": 22.0, "avg60": 15.0, "avg300": 10.0, "total": 200000},
					"full": map[string]any{"avg10": 10.0, "avg60": 7.5, "avg300": 4.0, "total": 120000},
				},
			},
			"io": map[string]any{
				"psi": map[string]any{
					"some": map[string]any{"avg10": 30.0, "avg60": 20.0, "avg300": 12.0, "total": 300000},
					"full": map[string]any{"avg10": 15.0, "avg60": 10.0, "avg300": 6.0, "total": 180000},
				},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "/api/v1/nodes/test-node/proxy/stats/summary"
		if r.URL.Path != expected {
			t.Errorf("Unexpected request path: got %s, want %s", r.URL.Path, expected)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	clientset := newTestClientset(t, handler)
	fetcher := NewFetcher(clientset)

	result, err := fetcher.FetchNodePSI(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("FetchNodePSI returned error: %v", err)
	}

	assertFloat(t, "CPU.Some.Avg10", 12.5, result.CPU.Some.Avg10)
	assertFloat(t, "CPU.Some.Avg60", 8.3, result.CPU.Some.Avg60)
	assertFloat(t, "CPU.Some.Avg300", 5.1, result.CPU.Some.Avg300)
	assertFloat(t, "CPU.Full.Avg10", 3.2, result.CPU.Full.Avg10)

	assertFloat(t, "Memory.Some.Avg10", 22.0, result.Memory.Some.Avg10)
	assertFloat(t, "Memory.Full.Avg10", 10.0, result.Memory.Full.Avg10)

	assertFloat(t, "IO.Some.Avg10", 30.0, result.IO.Some.Avg10)
	assertFloat(t, "IO.Full.Avg10", 15.0, result.IO.Full.Avg10)
}

func TestFetchNodePSI_NilPSIFields(t *testing.T) {
	response := map[string]any{
		"node": map[string]any{
			"cpu":    map[string]any{},
			"memory": map[string]any{},
			"io":     map[string]any{},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	clientset := newTestClientset(t, handler)
	fetcher := NewFetcher(clientset)

	result, err := fetcher.FetchNodePSI(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("FetchNodePSI returned error: %v", err)
	}

	assertFloat(t, "CPU.Some.Avg10", 0, result.CPU.Some.Avg10)
	assertFloat(t, "Memory.Some.Avg10", 0, result.Memory.Some.Avg10)
	assertFloat(t, "IO.Some.Avg10", 0, result.IO.Some.Avg10)
}

func TestFetchNodePSI_PartialPSI(t *testing.T) {
	response := map[string]any{
		"node": map[string]any{
			"cpu": map[string]any{
				"psi": map[string]any{
					"some": map[string]any{"avg10": 45.0, "avg60": 0.0, "avg300": 0.0, "total": 0},
					"full": map[string]any{"avg10": 0.0, "avg60": 0.0, "avg300": 0.0, "total": 0},
				},
			},
			"memory": map[string]any{},
			"io":     map[string]any{},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	clientset := newTestClientset(t, handler)
	fetcher := NewFetcher(clientset)

	result, err := fetcher.FetchNodePSI(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("FetchNodePSI returned error: %v", err)
	}

	assertFloat(t, "CPU.Some.Avg10", 45.0, result.CPU.Some.Avg10)
	assertFloat(t, "Memory.Some.Avg10", 0, result.Memory.Some.Avg10)
	assertFloat(t, "IO.Some.Avg10", 0, result.IO.Some.Avg10)
}

func TestFetchNodePSI_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	})

	clientset := newTestClientset(t, handler)
	fetcher := NewFetcher(clientset)

	_, err := fetcher.FetchNodePSI(context.Background(), "test-node")
	if err == nil {
		t.Fatal("Expected error from server 500, got nil")
	}
}

func TestFetchNodePSI_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`))
	})

	clientset := newTestClientset(t, handler)
	fetcher := NewFetcher(clientset)

	_, err := fetcher.FetchNodePSI(context.Background(), "test-node")
	if err == nil {
		t.Fatal("Expected error from invalid JSON, got nil")
	}
}

func TestFetchNodePSI_EmptyResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})

	clientset := newTestClientset(t, handler)
	fetcher := NewFetcher(clientset)

	result, err := fetcher.FetchNodePSI(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("FetchNodePSI returned error: %v", err)
	}

	assertFloat(t, "CPU.Some.Avg10", 0, result.CPU.Some.Avg10)
	assertFloat(t, "Memory.Some.Avg10", 0, result.Memory.Some.Avg10)
	assertFloat(t, "IO.Some.Avg10", 0, result.IO.Some.Avg10)
}

func TestFetchNodePSI_NodeNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","status":"Failure","message":"nodes \"missing-node\" not found","reason":"NotFound","code":404}`))
	})

	clientset := newTestClientset(t, handler)
	fetcher := NewFetcher(clientset)

	_, err := fetcher.FetchNodePSI(context.Background(), "missing-node")
	if err == nil {
		t.Fatal("Expected error for missing node, got nil")
	}
}

func assertFloat(t *testing.T, field string, want, got float64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}
