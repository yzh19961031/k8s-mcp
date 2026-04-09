package k8s

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// newManagerWithFakes 直接注入 fake client，绕过真实 kubeconfig
func newManagerWithFakes(clients map[string]kubernetes.Interface) *ClusterManager {
	m := &ClusterManager{
		clients: clients,
		errors:  make(map[string]error),
		configs: make(map[string]*rest.Config),
	}
	return m
}

func TestListNames(t *testing.T) {
	m := newManagerWithFakes(map[string]kubernetes.Interface{
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
	m := newManagerWithFakes(map[string]kubernetes.Interface{
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
	m := newManagerWithFakes(map[string]kubernetes.Interface{
		"prod": fake.NewSimpleClientset(),
	})

	_, err := m.Get("staging")
	if err == nil {
		t.Fatal("expected error for unknown cluster")
	}
	// 错误信息应包含可用集群列表
	if !strings.Contains(err.Error(), "prod") {
		t.Errorf("error should mention available clusters, got: %v", err)
	}
}

func TestGetConfig_Found(t *testing.T) {
	cfg := &rest.Config{Host: "https://example.com"}
	m := &ClusterManager{
		clients: map[string]kubernetes.Interface{},
		configs: map[string]*rest.Config{"prod": cfg},
		errors:  make(map[string]error),
	}

	got, err := m.GetConfig("prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Host != "https://example.com" {
		t.Errorf("expected host https://example.com, got %s", got.Host)
	}
}

func TestGetConfig_NotFound(t *testing.T) {
	m := &ClusterManager{
		clients: map[string]kubernetes.Interface{},
		configs: make(map[string]*rest.Config),
		errors:  make(map[string]error),
	}

	_, err := m.GetConfig("staging")
	if err == nil {
		t.Fatal("expected error for unknown cluster")
	}
}

func TestListErrors(t *testing.T) {
	m := &ClusterManager{
		clients: map[string]kubernetes.Interface{},
		configs: make(map[string]*rest.Config),
		errors: map[string]error{
			"broken": fmt.Errorf("connection refused"),
		},
	}

	errs := m.ListErrors()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs["broken"] != "connection refused" {
		t.Errorf("unexpected error message: %s", errs["broken"])
	}
}
