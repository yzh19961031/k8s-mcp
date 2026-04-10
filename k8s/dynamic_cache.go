package k8s

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// DynamicEntry 缓存单个集群的 dynamic client 和 RESTMapper
type DynamicEntry struct {
	Client dynamic.Interface
	Mapper meta.RESTMapper
}

// configProvider 是 DynamicClientCache 依赖的最小接口
type configProvider interface {
	GetConfig(clusterName string) (*rest.Config, error)
}

// DynamicClientCache 按集群懒初始化并缓存 DynamicEntry。
// 首次 Get 时创建 dynamic client + RESTMapper（需要 API Discovery 调用），
// 后续同集群调用直接返回缓存，无额外 API 请求。
type DynamicClientCache struct {
	cache sync.Map // string → *DynamicEntry
	mgr   configProvider
}

// NewDynamicClientCache 创建缓存，mgr 通常是 *ClusterManager
func NewDynamicClientCache(mgr configProvider) *DynamicClientCache {
	return &DynamicClientCache{mgr: mgr}
}

// Get 返回指定集群的 DynamicEntry，首次调用时初始化
func (c *DynamicClientCache) Get(clusterName string) (*DynamicEntry, error) {
	if v, ok := c.cache.Load(clusterName); ok {
		return v.(*DynamicEntry), nil
	}

	entry, err := c.buildEntry(clusterName)
	if err != nil {
		return nil, err
	}

	actual, _ := c.cache.LoadOrStore(clusterName, entry)
	return actual.(*DynamicEntry), nil
}

func (c *DynamicClientCache) buildEntry(clusterName string) (*DynamicEntry, error) {
	cfg, err := c.mgr.GetConfig(clusterName)
	if err != nil {
		return nil, fmt.Errorf("获取集群 %q 配置失败: %w", clusterName, err)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 dynamic client 失败: %w", err)
	}

	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 discovery client 失败: %w", err)
	}

	groups, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		return nil, fmt.Errorf("获取 API groups 失败: %w", err)
	}

	return &DynamicEntry{
		Client: dynClient,
		Mapper: restmapper.NewDiscoveryRESTMapper(groups),
	}, nil
}
