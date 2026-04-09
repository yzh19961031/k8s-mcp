package tools

import (
	"encoding/json"
	"testing"
)

// mockClusterLister 用于测试的 mock 实现
type mockClusterLister struct {
	names  []string
	errors map[string]string
}

func (m *mockClusterLister) ListNames() []string           { return m.names }
func (m *mockClusterLister) ListErrors() map[string]string { return m.errors }

func TestBuildClusterList(t *testing.T) {
	mgr := &mockClusterLister{
		names:  []string{"prod", "dev"},
		errors: map[string]string{"staging": "connection refused"},
	}

	result := buildClusterList(mgr)

	var r ClusterListResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, result)
	}

	if r.Total != 3 {
		t.Errorf("expected total=3 (2 ok + 1 error), got %d", r.Total)
	}

	okCount := 0
	errCount := 0
	for _, item := range r.Items {
		if item.Status == "ok" {
			okCount++
		} else if item.Status == "error" {
			errCount++
			if item.Error == "" {
				t.Error("error cluster should have error message")
			}
		}
	}
	if okCount != 2 || errCount != 1 {
		t.Errorf("expected 2 ok + 1 error, got ok=%d err=%d", okCount, errCount)
	}
}
