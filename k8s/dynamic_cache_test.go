package k8s

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

type mockConfigProvider struct {
	configs map[string]*rest.Config
}

func (m *mockConfigProvider) GetConfig(name string) (*rest.Config, error) {
	cfg, ok := m.configs[name]
	if !ok {
		return nil, fmt.Errorf("集群 %q 不存在", name)
	}
	return cfg, nil
}

func TestDynamicClientCache_UnknownCluster(t *testing.T) {
	mgr := &mockConfigProvider{configs: map[string]*rest.Config{}}
	cache := NewDynamicClientCache(mgr)

	_, err := cache.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown cluster, got nil")
	}
}

func TestDynamicClientCache_ErrorContainsClusterName(t *testing.T) {
	mgr := &mockConfigProvider{configs: map[string]*rest.Config{}}
	cache := NewDynamicClientCache(mgr)

	_, err := cache.Get("my-cluster")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "my-cluster") {
		t.Errorf("error should mention cluster name, got: %v", err)
	}
}
