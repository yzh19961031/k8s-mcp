package k8s

import (
	"sort"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// newManagerWithFakes 直接注入 fake client，绕过真实 kubeconfig
func newManagerWithFakes(clients map[string]kubernetesClient) *ClusterManager {
	m := &ClusterManager{
		clients: clients,
		errors:  make(map[string]error),
		configs: make(map[string]*rest.Config),
	}
	return m
}

func TestListNames(t *testing.T) {
	m := newManagerWithFakes(map[string]kubernetesClient{
		"prod": fake.NewSimpleClientset(),
		"dev":  fake.NewSimpleClientset(),
	})

	names := m.ListNames()
	sort.Strings(names)

	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
	if names[0] != "dev" || names[1] != "prod" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestGet_Found(t *testing.T) {
	fc := fake.NewSimpleClientset()
	m := newManagerWithFakes(map[string]kubernetesClient{
		"prod": fc,
	})

	client, err := m.Get("prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestGet_NotFound(t *testing.T) {
	m := newManagerWithFakes(map[string]kubernetesClient{
		"prod": fake.NewSimpleClientset(),
	})

	_, err := m.Get("staging")
	if err == nil {
		t.Fatal("expected error for unknown cluster")
	}
	// 错误信息应包含可用集群列表
	if !containsSubstr(err.Error(), "prod") {
		t.Errorf("error should mention available clusters, got: %v", err)
	}
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
