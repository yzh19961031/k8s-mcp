package k8s

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/yuanzhihao/k8s-mcp/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ClusterManager 管理多集群 k8s client
type ClusterManager struct {
	clients map[string]kubernetes.Interface
	configs map[string]*rest.Config
	errors  map[string]error
	mu      sync.RWMutex
}

// NewClusterManager 扫描 kubeconfigDir，并发初始化所有集群连接
func NewClusterManager(kubeconfigDir string) (*ClusterManager, error) {
	kubeconfigs, err := config.LoadKubeconfigs(kubeconfigDir)
	if err != nil {
		return nil, err
	}
	if len(kubeconfigs) == 0 {
		return nil, fmt.Errorf("目录 %q 下未找到 kubeconfig 文件（.yaml/.yml）", kubeconfigDir)
	}

	m := &ClusterManager{
		clients: make(map[string]kubernetes.Interface),
		configs: make(map[string]*rest.Config),
		errors:  make(map[string]error),
	}

	type result struct {
		name   string
		client kubernetes.Interface
		config *rest.Config
		err    error
	}

	ch := make(chan result, len(kubeconfigs))
	for name, path := range kubeconfigs {
		go func(n, p string) {
			cfg, err := clientcmd.BuildConfigFromFlags("", p)
			if err != nil {
				ch <- result{name: n, err: fmt.Errorf("解析 kubeconfig 失败: %w", err)}
				return
			}
			client, err := kubernetes.NewForConfig(cfg)
			if err != nil {
				ch <- result{name: n, err: fmt.Errorf("创建 client 失败: %w", err)}
				return
			}
			ch <- result{name: n, client: client, config: cfg}
		}(name, path)
	}

	for range kubeconfigs {
		r := <-ch
		if r.err != nil {
			m.errors[r.name] = r.err
		} else {
			m.clients[r.name] = r.client
			m.configs[r.name] = r.config
		}
	}

	return m, nil
}

// Get 返回指定集群的 client，未找到时返回包含可用集群列表的错误
func (m *ClusterManager) Get(clusterName string) (kubernetes.Interface, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, ok := m.clients[clusterName]
	if !ok {
		available := m.listNamesLocked()
		sort.Strings(available)
		return nil, fmt.Errorf("集群 %q 不存在，可用集群: [%s]", clusterName, strings.Join(available, ", "))
	}
	return client, nil
}

// GetConfig 返回指定集群的 rest.Config，供 dynamic client 使用
func (m *ClusterManager) GetConfig(clusterName string) (*rest.Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, ok := m.configs[clusterName]
	if !ok {
		return nil, fmt.Errorf("集群 %q 的配置不存在", clusterName)
	}
	return cfg, nil
}

// ListNames 返回所有已成功连接的集群名列表
func (m *ClusterManager) ListNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listNamesLocked()
}

func (m *ClusterManager) listNamesLocked() []string {
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// ListErrors 返回连接失败的集群及错误信息
func (m *ClusterManager) ListErrors() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	errs := make(map[string]string, len(m.errors))
	for name, err := range m.errors {
		errs[name] = err.Error()
	}
	return errs
}

// Reload 重新扫描 kubeconfig 目录并更新连接池（不重启服务）
func (m *ClusterManager) Reload(kubeconfigDir string) error {
	newMgr, err := NewClusterManager(kubeconfigDir)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients = newMgr.clients
	m.configs = newMgr.configs
	m.errors = newMgr.errors
	return nil
}
