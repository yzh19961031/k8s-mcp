# k8s-mcp 重构实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 重构 k8s-mcp，缓存 RESTMapper 消除重复 Discovery 开销，用通用 `list_resources` 取代低频 list 工具，修复 Pod 状态显示，抽取 Handler 样板代码。

**Architecture:** 新增 `k8s.DynamicClientCache` 按集群懒初始化并缓存 `dynamic.Interface + RESTMapper`；`tools/resource.go` 改为依赖此缓存；新增 `list_resources` 通用工具支持任意 Kind；Handler 样板抽取到 `helpers.go`；`podDisplayStatus` 实现 kubectl 风格完整状态判断。

**Tech Stack:** Go 1.25, `k8s.io/client-go` (dynamic, discovery, fake), `k8s.io/apimachinery`, `github.com/mark3labs/mcp-go`

---

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `k8s/dynamic_cache.go` | 新增 | DynamicClientCache + DynamicEntry |
| `k8s/dynamic_cache_test.go` | 新增 | 缓存错误路径测试 |
| `tools/helpers.go` | 修改 | 新增 mustString / getClusterClient |
| `tools/helpers_test.go` | 修改 | 新增 helper 测试 |
| `tools/types.go` | 修改 | 新增 ResourceBrief/ResourceListResult；删 ReplicaSetBrief/ReplicaSetListResult |
| `tools/pod.go` | 修改 | podDisplayStatus kubectl 风格；加 previous 参数 |
| `tools/pod_test.go` | 修改 | 新增 podDisplayStatus 测试；更新 getPodLogsImpl 测试 |
| `tools/resource.go` | 修改 | 删 buildDynamic；接入 DynamicClientCache；新增 list_resources；修 annotations |
| `tools/deployment.go` | 修改 | 删 list_replicasets；handler 改用 helper |
| `tools/node.go` | 修改 | handler 改用 helper |
| `tools/namespace.go` | 修改 | handler 改用 helper |
| `tools/event.go` | 修改 | handler 改用 helper |
| `tools/pod.go` | 修改 | handler 改用 helper（exec_pod 除外，它需要 GetConfig） |
| `main.go` | 修改 | 初始化 DynamicClientCache，更新 RegisterResourceTools 签名 |
| `README.md` | 修改 | 更新工具列表，删 list_replicasets，加 list_resources |

---

## Task 1: DynamicClientCache

**Files:**
- Create: `k8s/dynamic_cache.go`
- Create: `k8s/dynamic_cache_test.go`

- [ ] **Step 1: 写失败测试**

```go
// k8s/dynamic_cache_test.go
package k8s

import (
	"fmt"
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
	// 错误信息应包含集群名，便于调试
	if !contains(err.Error(), "my-cluster") {
		t.Errorf("error should mention cluster name, got: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
cd /Volumes/data/personal/code/k8s-mcp
go test ./k8s/... -run TestDynamicClientCache -v
```

预期：`NewDynamicClientCache undefined`

- [ ] **Step 3: 实现 DynamicClientCache**

```go
// k8s/dynamic_cache.go
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

	// LoadOrStore 防止并发重复创建，取第一个存入的
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
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./k8s/... -run TestDynamicClientCache -v
```

预期：`PASS`

- [ ] **Step 5: 运行全量测试，确认无回归**

```bash
go test ./...
```

预期：所有原有测试通过

- [ ] **Step 6: 提交**

```bash
cd /Volumes/data/personal/code/k8s-mcp
git add k8s/dynamic_cache.go k8s/dynamic_cache_test.go
git commit -m "feat: 新增 DynamicClientCache，按集群缓存 RESTMapper"
```

---

## Task 2: Helper 函数

**Files:**
- Modify: `tools/helpers.go`
- Modify: `tools/helpers_test.go`

- [ ] **Step 1: 写失败测试**

在 `tools/helpers_test.go` 末尾追加：

```go
func TestMustString_Valid(t *testing.T) {
	args := map[string]any{"cluster": "prod"}
	got, err := mustString(args, "cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "prod" {
		t.Errorf("expected 'prod', got %q", got)
	}
}

func TestMustString_Missing(t *testing.T) {
	args := map[string]any{}
	_, err := mustString(args, "cluster")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestMustString_Empty(t *testing.T) {
	args := map[string]any{"cluster": ""}
	_, err := mustString(args, "cluster")
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}

func TestMustString_WrongType(t *testing.T) {
	args := map[string]any{"cluster": 123}
	_, err := mustString(args, "cluster")
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
go test ./tools/... -run TestMustString -v
```

预期：`mustString undefined`

- [ ] **Step 3: 实现 helper 函数**

在 `tools/helpers.go` 末尾追加（保留原有三个函数不动）：

```go
// mustString 从 args 中提取必填字符串参数，为空或类型错误时返回 error
func mustString(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("参数 %s 无效", key)
	}
	return v, nil
}

// getClusterClient 从 args 提取 cluster 参数并获取对应的 kubernetes.Interface
func getClusterClient(args map[string]any, mgr ClusterManagerInterface) (kubernetes.Interface, string, error) {
	cluster, err := mustString(args, "cluster")
	if err != nil {
		return nil, "", err
	}
	client, err := mgr.Get(cluster)
	if err != nil {
		return nil, "", err
	}
	return client, cluster, nil
}
```

同时在 `tools/helpers.go` 顶部 import 块中补充：

```go
import (
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
)
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./tools/... -run TestMustString -v
```

预期：`PASS`

- [ ] **Step 5: 提交**

```bash
git add tools/helpers.go tools/helpers_test.go
git commit -m "feat: 新增 mustString/getClusterClient helper，消除 handler 样板代码"
```

---

## Task 3: Pod 状态判断修复 + previous 参数

**Files:**
- Modify: `tools/pod.go`
- Modify: `tools/pod_test.go`

- [ ] **Step 1: 写失败测试**

在 `tools/pod_test.go` 的 import 块中补充 `metav1` 和 `time`（已有则跳过），然后追加测试：

```go
func makePodWithStatus(name string, deletionTimestamp *metav1.Time, initStatuses []corev1.ContainerStatus, containerStatuses []corev1.ContainerStatus, phase string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			DeletionTimestamp: deletionTimestamp,
		},
		Status: corev1.PodStatus{
			Phase:                 corev1.PodPhase(phase),
			InitContainerStatuses: initStatuses,
			ContainerStatuses:     containerStatuses,
		},
	}
}

func TestPodDisplayStatus(t *testing.T) {
	now := metav1.Now()

	cases := []struct {
		name     string
		pod      corev1.Pod
		expected string
	}{
		{
			name: "terminating",
			pod:  makePodWithStatus("p", &now, nil, nil, "Running"),
			expected: "Terminating",
		},
		{
			name: "crash loop backoff",
			pod: makePodWithStatus("p", nil, nil, []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			}, "Running"),
			expected: "CrashLoopBackOff",
		},
		{
			name: "image pull backoff",
			pod: makePodWithStatus("p", nil, nil, []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
					},
				},
			}, "Pending"),
			expected: "ImagePullBackOff",
		},
		{
			name: "oom killed",
			pod: makePodWithStatus("p", nil, nil, []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 137,
							Reason:   "OOMKilled",
						},
					},
				},
			}, "Failed"),
			expected: "OOMKilled",
		},
		{
			name: "init crash loop",
			pod: makePodWithStatus("p", nil, []corev1.ContainerStatus{
				{
					RestartCount: 3,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			}, nil, "Pending"),
			expected: "Init:CrashLoopBackOff",
		},
		{
			name: "running normally",
			pod: makePodWithStatus("p", nil, nil, []corev1.ContainerStatus{
				{Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			}, "Running"),
			expected: "Running",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := podDisplayStatus(tc.pod)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestGetPodLogsImpl_Previous(t *testing.T) {
	pod := makePod("web-1", "default", "Running", 1, 1, 0, "node-1")
	fc := fake.NewSimpleClientset(&pod)

	// previous=true 不报错即可（fake client 返回空日志）
	result := getPodLogsImpl(context.Background(), fc, "prod", "default", "web-1", "", 50, true)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, result)
	}
	if _, ok := m["logs"]; !ok {
		t.Error("expected 'logs' key in result")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
go test ./tools/... -run "TestPodDisplayStatus|TestGetPodLogsImpl_Previous" -v
```

预期：`podDisplayStatus undefined`，`getPodLogsImpl` 参数个数不匹配

- [ ] **Step 3: 实现 podDisplayStatus，修改 getPodLogsImpl**

在 `tools/pod.go` 中，将 `listPodsImpl` 里的 `Status: string(p.Status.Phase)` 改为：

```go
Status: podDisplayStatus(p),
```

在 `getPodLogsImpl` 函数签名改为：

```go
func getPodLogsImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace, pod, container string, tail int64, previous bool) string {
	opts := &corev1.PodLogOptions{TailLines: &tail, Previous: previous}
	if container != "" {
		opts.Container = container
	}
	// ...（其余不变）
}
```

并在 `RegisterPodTools` 的 `get_pod_logs` 工具定义中追加参数：

```go
mcp.WithString("previous", mcp.Description("为 true 时获取已终止容器的历史日志")),
```

handler 中提取参数：

```go
previous := false
if p, ok := args["previous"].(string); ok && p == "true" {
    previous = true
}
```

调用时改为：

```go
return mcp.NewToolResultText(getPodLogsImpl(ctx, client, cluster, namespace, pod, container, tail, previous)), nil
```

在文件末尾（`containerState` 函数之后）新增 `podDisplayStatus`：

```go
// podDisplayStatus 返回与 kubectl 一致的 Pod 状态字符串
func podDisplayStatus(p corev1.Pod) string {
	// 1. 正在删除
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}

	// 2. Init 容器未完成
	for i, cs := range p.Status.InitContainerStatuses {
		if cs.State.Terminated != nil && cs.State.Terminated.ExitCode == 0 {
			continue // 已正常完成
		}
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if reason != "" && reason != "PodInitializing" {
				return "Init:" + reason
			}
		}
		if cs.State.Terminated != nil {
			if cs.State.Terminated.ExitCode != 0 {
				return fmt.Sprintf("Init:ExitCode:%d", cs.State.Terminated.ExitCode)
			}
		}
		if cs.RestartCount > 0 {
			return "Init:CrashLoopBackOff"
		}
		return fmt.Sprintf("Init:%d/%d", i, len(p.Spec.InitContainers))
	}

	// 3. 普通容器状态
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
		if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			if cs.State.Terminated.Reason != "" {
				return cs.State.Terminated.Reason
			}
			return "Error"
		}
	}

	// 4. 兜底
	if p.Status.Phase == "" {
		return "Unknown"
	}
	return string(p.Status.Phase)
}
```

同时删除 `pod.go` 中原来已被弃用的 `countRestarts` 中对 status phase 的直接使用（只需在 `listPodsImpl` 里把 `string(p.Status.Phase)` 改为 `podDisplayStatus(p)` 即可，`countRestarts` 不需要改）。

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./tools/... -run "TestPodDisplayStatus|TestGetPodLogsImpl|TestListPodsImpl|TestDescribePodImpl" -v
```

预期：所有测试 `PASS`

- [ ] **Step 5: 提交**

```bash
git add tools/pod.go tools/pod_test.go
git commit -m "fix: Pod 状态显示改用 kubectl 风格判断；get_pod_logs 支持 previous 参数"
```

---

## Task 4: types.go 更新

**Files:**
- Modify: `tools/types.go`

- [ ] **Step 1: 在 types.go 中删除 ReplicaSetBrief/ReplicaSetListResult，新增 ResourceBrief/ResourceListResult**

找到并删除以下代码块（`types.go:144-160`）：

```go
// ReplicaSetBrief RS 摘要
type ReplicaSetBrief struct { ... }

// ReplicaSetListResult list_replicasets 返回值
type ReplicaSetListResult struct { ... }
```

在文件末尾（`ResourceQuotaListResult` 之后）追加：

```go
// ResourceBrief 通用资源摘要（list_resources 使用）
type ResourceBrief struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Age       string            `json:"age"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// ResourceListResult list_resources 返回值
type ResourceListResult struct {
	Cluster   string         `json:"cluster"`
	Kind      string         `json:"kind"`
	Namespace string         `json:"namespace"`
	Total     int            `json:"total"`
	Items     []ResourceBrief `json:"items"`
}
```

- [ ] **Step 2: 确认编译通过**

```bash
go build ./...
```

预期：编译报错（deployment.go 还引用 ReplicaSetBrief/ReplicaSetListResult，下一个 task 修）

记录错误信息后继续下一步。

- [ ] **Step 3: 提交（等 deployment.go 修完后一起提交）**

暂不提交，等 Task 5 完成后一起提交。

---

## Task 5: 删除 list_replicasets + Handler 改用 helper

**Files:**
- Modify: `tools/deployment.go`
- Modify: `tools/node.go`
- Modify: `tools/namespace.go`
- Modify: `tools/event.go`

- [ ] **Step 1: 修改 deployment.go**

删除 `list_replicasets` 的注册代码（`RegisterDeploymentTools` 中的第一个 `s.AddTool`）以及 `listReplicaSetsImpl` 函数（整个函数体）。

将 `list_deployments` 和 `scale_deployment` 的 handler 改用 helper：

```go
// RegisterDeploymentTools 注册 Deployment 相关 MCP 工具
func RegisterDeploymentTools(s *server.MCPServer, mgr ClusterManagerInterface) {
	s.AddTool(mcp.NewTool("list_deployments",
		mcp.WithDescription("列出指定集群和命名空间的 Deployment"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		client, cluster, err := getClusterClient(args, mgr)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, err := mustString(args, "namespace")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listDeploymentsImpl(ctx, client, cluster, namespace)), nil
	})

	s.AddTool(mcp.NewTool("scale_deployment",
		mcp.WithDescription("调整指定 Deployment 的副本数"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Deployment 名称")),
		mcp.WithNumber("replicas", mcp.Required(), mcp.Description("目标副本数")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		client, cluster, err := getClusterClient(args, mgr)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, err := mustString(args, "namespace")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		name, err := mustString(args, "name")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		replicasF, ok := args["replicas"].(float64)
		if !ok {
			return mcp.NewToolResultText(toolError("参数 replicas 无效")), nil
		}
		return mcp.NewToolResultText(scaleDeploymentImpl(ctx, client, cluster, namespace, name, int32(replicasF))), nil
	})
}
```

- [ ] **Step 2: 修改 node.go 的两个 handler**

将 `RegisterNodeTools` 中两个 handler 改为：

```go
func RegisterNodeTools(s *server.MCPServer, mgr ClusterManagerInterface) {
	s.AddTool(mcp.NewTool("list_nodes",
		mcp.WithDescription("列出指定集群的所有节点及状态"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, cluster, err := getClusterClient(req.GetArguments(), mgr)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listNodesImpl(ctx, client, cluster)), nil
	})

	s.AddTool(mcp.NewTool("describe_node",
		mcp.WithDescription("获取指定节点的详细信息"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("name", mcp.Required(), mcp.Description("节点名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		client, cluster, err := getClusterClient(args, mgr)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		name, err := mustString(args, "name")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(describeNodeImpl(ctx, client, cluster, name)), nil
	})
}
```

- [ ] **Step 3: 修改 namespace.go 的两个 handler**

```go
func RegisterNamespaceTools(s *server.MCPServer, mgr ClusterManagerInterface) {
	s.AddTool(mcp.NewTool("list_resource_quotas",
		mcp.WithDescription("列出指定命名空间的所有 ResourceQuota 及用量"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		client, cluster, err := getClusterClient(args, mgr)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, err := mustString(args, "namespace")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listResourceQuotasImpl(ctx, client, cluster, namespace)), nil
	})

	s.AddTool(mcp.NewTool("list_namespaces",
		mcp.WithDescription("列出指定集群的所有命名空间"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, cluster, err := getClusterClient(req.GetArguments(), mgr)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listNamespacesImpl(ctx, client, cluster)), nil
	})
}
```

- [ ] **Step 4: 修改 event.go 的 handler**

`namespace` 是可选参数，不用 mustString，只改 cluster 部分：

```go
func RegisterEventTools(s *server.MCPServer, mgr ClusterManagerInterface) {
	s.AddTool(mcp.NewTool("get_events",
		mcp.WithDescription("获取指定集群（和命名空间）的 K8s 事件，支持按资源过滤"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Description("命名空间，留空获取所有命名空间的事件")),
		mcp.WithNumber("limit", mcp.Description("返回数量上限，默认 20")),
		mcp.WithString("involved_object_kind", mcp.Description("按资源类型过滤，如 Pod、ReplicaSet、Deployment")),
		mcp.WithString("involved_object_name", mcp.Description("按资源名称过滤，如 my-pod-abc123")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		client, cluster, err := getClusterClient(args, mgr)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, _ := args["namespace"].(string)
		limit := 20
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		involvedKind, _ := args["involved_object_kind"].(string)
		involvedName, _ := args["involved_object_name"].(string)
		return mcp.NewToolResultText(getEventsImpl(ctx, client, cluster, namespace, involvedKind, involvedName, limit)), nil
	})
}
```

- [ ] **Step 5: 修改 pod.go 的 handler（list_pods / get_pod_logs / describe_pod，不动 exec_pod）**

`list_pods` handler：

```go
s.AddTool(mcp.NewTool("list_pods",
    mcp.WithDescription("列出指定集群和命名空间下的 Pod 摘要"),
    mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
    mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间，空字符串表示所有命名空间")),
    mcp.WithString("label_selector", mcp.Description("标签选择器，例如 app=nginx")),
    mcp.WithNumber("limit", mcp.Description("返回数量上限，默认 20")),
), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    args := req.GetArguments()
    client, cluster, err := getClusterClient(args, mgr)
    if err != nil {
        return mcp.NewToolResultText(toolError(err.Error())), nil
    }
    namespace, _ := args["namespace"].(string) // 允许空字符串（查所有命名空间）
    labelSelector, _ := args["label_selector"].(string)
    limit := 20
    if l, ok := args["limit"].(float64); ok && l > 0 {
        limit = int(l)
    }
    return mcp.NewToolResultText(listPodsImpl(ctx, client, cluster, namespace, labelSelector, limit)), nil
})
```

`get_pod_logs` handler：

```go
s.AddTool(mcp.NewTool("get_pod_logs",
    mcp.WithDescription("获取指定 Pod 的容器日志"),
    mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
    mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
    mcp.WithString("pod", mcp.Required(), mcp.Description("Pod 名称")),
    mcp.WithString("container", mcp.Description("容器名称，多容器时必填")),
    mcp.WithNumber("tail", mcp.Description("返回最后 N 行，默认 100")),
    mcp.WithString("previous", mcp.Description("为 true 时获取已终止容器的历史日志")),
), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    args := req.GetArguments()
    client, cluster, err := getClusterClient(args, mgr)
    if err != nil {
        return mcp.NewToolResultText(toolError(err.Error())), nil
    }
    namespace, err := mustString(args, "namespace")
    if err != nil {
        return mcp.NewToolResultText(toolError(err.Error())), nil
    }
    pod, err := mustString(args, "pod")
    if err != nil {
        return mcp.NewToolResultText(toolError(err.Error())), nil
    }
    container, _ := args["container"].(string)
    tail := int64(100)
    if t, ok := args["tail"].(float64); ok && t > 0 {
        tail = int64(t)
    }
    previous := false
    if p, ok := args["previous"].(string); ok && p == "true" {
        previous = true
    }
    return mcp.NewToolResultText(getPodLogsImpl(ctx, client, cluster, namespace, pod, container, tail, previous)), nil
})
```

`describe_pod` handler：

```go
s.AddTool(mcp.NewTool("describe_pod",
    mcp.WithDescription("获取指定 Pod 的详细信息"),
    mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
    mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
    mcp.WithString("name", mcp.Required(), mcp.Description("Pod 名称")),
), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    args := req.GetArguments()
    client, cluster, err := getClusterClient(args, mgr)
    if err != nil {
        return mcp.NewToolResultText(toolError(err.Error())), nil
    }
    namespace, err := mustString(args, "namespace")
    if err != nil {
        return mcp.NewToolResultText(toolError(err.Error())), nil
    }
    name, err := mustString(args, "name")
    if err != nil {
        return mcp.NewToolResultText(toolError(err.Error())), nil
    }
    return mcp.NewToolResultText(describePodImpl(ctx, client, cluster, namespace, name)), nil
})
```

- [ ] **Step 6: 确认编译通过**

```bash
go build ./...
```

预期：`PASS`（types.go 中删了 ReplicaSetBrief，deployment.go 也删了 listReplicaSetsImpl）

- [ ] **Step 7: 运行全量测试**

```bash
go test ./...
```

预期：所有测试通过

- [ ] **Step 8: 提交**

```bash
git add tools/types.go tools/deployment.go tools/node.go tools/namespace.go tools/event.go tools/pod.go
git commit -m "refactor: 删 list_replicasets；handler 改用 helper 函数；新增 ResourceBrief/ResourceListResult 类型"
```

---

## Task 6: resource.go 重构（接入缓存 + list_resources + annotations 修复）

**Files:**
- Modify: `tools/resource.go`

- [ ] **Step 1: 替换 resource.go 全文**

用以下完整内容替换 `tools/resource.go`：

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/yuanzhihao/k8s-mcp/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/types"
)

// noisyAnnotations 是描述资源时需要过滤掉的高噪音 annotation key
var noisyAnnotations = []string{
	"kubectl.kubernetes.io/last-applied-configuration",
	"deployment.kubernetes.io/revision",
}

// wellKnownGroups 为常见资源类型预设 API Group，避免 discovery mapper 因 Group 为空而匹配失败
var wellKnownGroups = map[string]string{
	"deployment":              "apps",
	"replicaset":              "apps",
	"statefulset":             "apps",
	"daemonset":               "apps",
	"controllerrevision":      "apps",
	"job":                     "batch",
	"cronjob":                 "batch",
	"ingress":                 "networking.k8s.io",
	"networkpolicy":           "networking.k8s.io",
	"horizontalpodautoscaler": "autoscaling",
	"poddisruptionbudget":     "policy",
	"clusterrole":             "rbac.authorization.k8s.io",
	"clusterrolebinding":      "rbac.authorization.k8s.io",
	"role":                    "rbac.authorization.k8s.io",
	"rolebinding":             "rbac.authorization.k8s.io",
	"storageclass":            "storage.k8s.io",
	"volumeattachment":        "storage.k8s.io",
}

// resolveGVR 通过 RESTMapper 将 kind 字符串解析为 GVR 和是否 namespaced
func resolveGVR(mapper interface {
	RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error)
}, kind string) (schema.GroupVersionResource, bool, error) {
	group := wellKnownGroups[strings.ToLower(kind)]
	mappings, err := mapper.RESTMappings(schema.GroupKind{Group: group, Kind: kind})
	if err != nil || len(mappings) == 0 {
		return schema.GroupVersionResource{}, false, fmt.Errorf("找不到 Kind %q 的 REST mapping", kind)
	}
	mapping := mappings[0]
	return mapping.Resource, mapping.Scope.Name() == "namespace", nil
}

// RegisterResourceTools 注册资源操作 MCP 工具
func RegisterResourceTools(s *server.MCPServer, cache *k8s.DynamicClientCache) {
	// list_resources
	s.AddTool(mcp.NewTool("list_resources",
		mcp.WithDescription("列出任意 K8s 资源（通用）。支持 Service/Ingress/PVC/ConfigMap/StatefulSet/DaemonSet 等所有 Kind"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Service、Ingress、PersistentVolumeClaim、ConfigMap")),
		mcp.WithString("namespace", mcp.Description("命名空间，留空查所有命名空间")),
		mcp.WithString("label_selector", mcp.Description("标签过滤，如 app=nginx")),
		mcp.WithNumber("limit", mcp.Description("返回数量上限，默认 20")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, err := mustString(args, "cluster")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		kind, err := mustString(args, "kind")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, _ := args["namespace"].(string)
		labelSelector, _ := args["label_selector"].(string)
		limit := 20
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		entry, err := cache.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listResourcesImpl(ctx, entry, cluster, kind, namespace, labelSelector, limit)), nil
	})

	// describe_resource
	s.AddTool(mcp.NewTool("describe_resource",
		mcp.WithDescription("获取任意 K8s 资源的详细信息（通用）"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间，集群级资源传空字符串")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Deployment、Service、ConfigMap")),
		mcp.WithString("name", mcp.Required(), mcp.Description("资源名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, err := mustString(args, "cluster")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, _ := args["namespace"].(string)
		kind, err := mustString(args, "kind")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		name, err := mustString(args, "name")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		entry, err := cache.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(describeResourceImpl(ctx, entry, cluster, namespace, kind, name)), nil
	})

	// apply_manifest
	s.AddTool(mcp.NewTool("apply_manifest",
		mcp.WithDescription("应用 YAML 清单到指定集群（server-side apply）"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("yaml_content", mcp.Required(), mcp.Description("YAML 或 JSON 格式的资源清单")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, err := mustString(args, "cluster")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		yamlContent, err := mustString(args, "yaml_content")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		entry, err := cache.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(applyManifestImpl(ctx, entry, cluster, yamlContent)), nil
	})

	// delete_resource
	s.AddTool(mcp.NewTool("delete_resource",
		mcp.WithDescription("删除指定集群中的 K8s 资源"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间，集群级资源传空字符串")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Pod、Deployment")),
		mcp.WithString("name", mcp.Required(), mcp.Description("资源名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, err := mustString(args, "cluster")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, _ := args["namespace"].(string)
		kind, err := mustString(args, "kind")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		name, err := mustString(args, "name")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		entry, err := cache.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(deleteResourceImpl(ctx, entry, cluster, namespace, kind, name)), nil
	})
}

func listResourcesImpl(ctx context.Context, entry *k8s.DynamicEntry, cluster, kind, namespace, labelSelector string, limit int) string {
	gvr, namespaced, err := resolveGVR(entry.Mapper, kind)
	if err != nil {
		return toolError(err.Error())
	}

	opts := metav1.ListOptions{LabelSelector: labelSelector, Limit: int64(limit)}

	var list *unstructured.UnstructuredList
	if namespaced {
		list, err = entry.Client.Resource(gvr).Namespace(namespace).List(ctx, opts)
	} else {
		list, err = entry.Client.Resource(gvr).List(ctx, opts)
	}
	if err != nil {
		return toolError(fmt.Sprintf("list %s 失败: %v", kind, err))
	}

	items := make([]ResourceBrief, 0, len(list.Items))
	for _, obj := range list.Items {
		age := ""
		ts := obj.GetCreationTimestamp()
		if !ts.IsZero() {
			age = ageString(ts.Time)
		}
		items = append(items, ResourceBrief{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Age:       age,
			Labels:    obj.GetLabels(),
		})
	}

	return toJSON(ResourceListResult{
		Cluster:   cluster,
		Kind:      kind,
		Namespace: namespace,
		Total:     len(items),
		Items:     items,
	})
}

func describeResourceImpl(ctx context.Context, entry *k8s.DynamicEntry, cluster, namespace, kind, name string) string {
	gvr, namespaced, err := resolveGVR(entry.Mapper, kind)
	if err != nil {
		return toolError(err.Error())
	}

	var obj *unstructured.Unstructured
	if namespaced {
		obj, err = entry.Client.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = entry.Client.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return toolError(fmt.Sprintf("get resource 失败: %v", err))
	}

	// 移除 managedFields 噪音
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")

	// 过滤高噪音 annotation，保留业务 annotation（如 hami.io/*）
	if annots, ok, _ := unstructured.NestedStringMap(obj.Object, "metadata", "annotations"); ok {
		for _, key := range noisyAnnotations {
			delete(annots, key)
		}
		_ = unstructured.SetNestedStringMap(obj.Object, annots, "metadata", "annotations")
	}

	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	status, _, _ := unstructured.NestedMap(obj.Object, "status")
	labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
	annots, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")

	age := ""
	if creationTime, ok, _ := unstructured.NestedString(obj.Object, "metadata", "creationTimestamp"); ok {
		if t, err := time.Parse(time.RFC3339, creationTime); err == nil {
			age = ageString(t)
		}
	}

	return toJSON(map[string]interface{}{
		"cluster":     cluster,
		"kind":        kind,
		"namespace":   namespace,
		"name":        name,
		"age":         age,
		"labels":      labels,
		"annotations": annots,
		"spec":        spec,
		"status":      status,
	})
}

func applyManifestImpl(ctx context.Context, entry *k8s.DynamicEntry, cluster, yamlContent string) string {
	obj := &unstructured.Unstructured{}
	dec := k8syaml.NewYAMLOrJSONDecoder(strings.NewReader(yamlContent), 4096)
	if err := dec.Decode(&obj.Object); err != nil {
		return toolError(fmt.Sprintf("解析 YAML 失败: %v", err))
	}

	kind := obj.GetKind()
	if kind == "" {
		return toolError("YAML 中缺少 kind 字段")
	}

	gvr, namespaced, err := resolveGVR(entry.Mapper, kind)
	if err != nil {
		return toolError(err.Error())
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return toolError(fmt.Sprintf("序列化对象失败: %v", err))
	}

	force := true
	var result *unstructured.Unstructured
	if namespaced {
		result, err = entry.Client.Resource(gvr).Namespace(obj.GetNamespace()).Patch(
			ctx, obj.GetName(), types.ApplyPatchType, data,
			metav1.PatchOptions{FieldManager: "k8s-mcp", Force: &force},
		)
	} else {
		result, err = entry.Client.Resource(gvr).Patch(
			ctx, obj.GetName(), types.ApplyPatchType, data,
			metav1.PatchOptions{FieldManager: "k8s-mcp", Force: &force},
		)
	}
	if err != nil {
		return toolError(fmt.Sprintf("apply 失败: %v", err))
	}

	return toJSON(map[string]interface{}{
		"cluster":     cluster,
		"kind":        result.GetKind(),
		"namespace":   result.GetNamespace(),
		"name":        result.GetName(),
		"api_version": result.GetAPIVersion(),
		"status":      "applied",
	})
}

func deleteResourceImpl(ctx context.Context, entry *k8s.DynamicEntry, cluster, namespace, kind, name string) string {
	gvr, namespaced, err := resolveGVR(entry.Mapper, kind)
	if err != nil {
		return toolError(err.Error())
	}

	if namespaced {
		err = entry.Client.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	} else {
		err = entry.Client.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{})
	}
	if err != nil {
		return toolError(fmt.Sprintf("delete resource 失败: %v", err))
	}

	return toJSON(map[string]interface{}{
		"cluster":   cluster,
		"kind":      kind,
		"namespace": namespace,
		"name":      name,
		"status":    "deleted",
	})
}
```

注意：`resolveGVR` 中的 `mapper` 参数类型需要用 `meta.RESTMapper` 接口，需在 import 中加入：

```go
"k8s.io/apimachinery/pkg/api/meta"
```

并将 `resolveGVR` 签名改为：

```go
func resolveGVR(mapper meta.RESTMapper, kind string) (schema.GroupVersionResource, bool, error) {
```

- [ ] **Step 2: 确认编译通过**

```bash
go build ./...
```

如果有 import 未使用或缺失，按编译错误提示修正。

- [ ] **Step 3: 运行全量测试**

```bash
go test ./...
```

预期：所有测试通过（resource.go 原本无单元测试，不影响）

- [ ] **Step 4: 提交**

```bash
git add tools/resource.go
git commit -m "refactor: resource.go 接入 DynamicClientCache；新增 list_resources；保留业务 annotations"
```

---

## Task 7: main.go 接线

**Files:**
- Modify: `main.go`

- [ ] **Step 1: 更新 main.go**

将 `main.go` 替换为：

```go
package main

import (
	"flag"
	"log"

	"github.com/mark3labs/mcp-go/server"
	"github.com/yuanzhihao/k8s-mcp/k8s"
	"github.com/yuanzhihao/k8s-mcp/tools"
)

func main() {
	kubeconfigDir := flag.String("kubeconfig-dir", "", "kubeconfig 文件目录（可选，文件名即集群名）")
	tokenConfig := flag.String("token-config", "", "token 配置文件路径（可选，同名集群优先于 kubeconfig）")
	flag.Parse()

	mgr, err := k8s.NewClusterManager(*kubeconfigDir, *tokenConfig)
	if err != nil {
		log.Fatalf("初始化集群管理器失败: %v", err)
	}

	for name, errMsg := range mgr.ListErrors() {
		log.Printf("警告: 集群 %q 连接失败: %s", name, errMsg)
	}

	dynCache := k8s.NewDynamicClientCache(mgr)

	s := server.NewMCPServer("k8s-mcp", "1.0.0",
		server.WithToolCapabilities(true),
	)

	tools.RegisterClusterTools(s, mgr)
	tools.RegisterPodTools(s, mgr)
	tools.RegisterNamespaceTools(s, mgr)
	tools.RegisterNodeTools(s, mgr)
	tools.RegisterDeploymentTools(s, mgr)
	tools.RegisterEventTools(s, mgr)
	tools.RegisterResourceTools(s, dynCache)

	log.Printf("k8s-mcp server 启动，已加载集群: %v", mgr.ListNames())

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("MCP Server 异常退出: %v", err)
	}
}
```

- [ ] **Step 2: 确认编译通过**

```bash
go build ./...
```

预期：`PASS`

- [ ] **Step 3: 运行全量测试**

```bash
go test ./...
```

预期：所有测试通过

- [ ] **Step 4: 提交**

```bash
git add main.go
git commit -m "feat: main.go 接入 DynamicClientCache，完成整体重构接线"
```

---

## Task 8: 更新 README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 更新工具清单表格**

将 README.md 中"工具清单"部分的"资源查询（只读）"表格替换为：

```markdown
### 资源查询（只读）

| 工具 | 参数 | 说明 |
|------|------|------|
| `list_namespaces` | cluster | 列出命名空间 |
| `list_nodes` | cluster | 列出节点及状态 |
| `describe_node` | cluster, name | 节点详情（容量/条件/地址） |
| `list_pods` | cluster, namespace, label_selector?, limit? | 列出 Pod 摘要（状态显示与 kubectl 一致） |
| `get_pod_logs` | cluster, namespace, pod, container?, tail?, previous? | 获取容器日志，previous=true 查历史日志 |
| `describe_pod` | cluster, namespace, name | Pod 详情（容器状态/条件） |
| `list_deployments` | cluster, namespace | 列出 Deployment |
| `list_resources` | cluster, kind, namespace?, label_selector?, limit? | 通用资源列表，支持 Service/Ingress/PVC/ConfigMap/StatefulSet 等任意 Kind |
| `describe_resource` | cluster, namespace, kind, name | 通用资源描述（保留业务 annotations，过滤噪音字段） |
| `get_events` | cluster, namespace?, limit?, involved_object_kind?, involved_object_name? | 获取 K8s 事件 |
| `list_resource_quotas` | cluster, namespace | 列出 ResourceQuota 及用量 |
```

- [ ] **Step 2: 更新工具总数**

将 `README.md` 第 11 行的工具数字从 `13 个` 改为 `13 个`（删掉 list_replicasets 加上 list_resources，总数不变，但内容不同）。同时更新该行描述：

```markdown
- **13 个 MCP 工具**：覆盖集群查询、Pod 操作、Deployment 管理、通用资源列举（list_resources 支持任意 Kind）、事件查看、资源操作
```

- [ ] **Step 3: 更新目录结构中 k8s/ 说明**

将目录结构中 `k8s/` 部分更新为：

```markdown
├── k8s/
│   ├── manager.go       # 多集群连接池（并发初始化、Get/Reload）
│   └── dynamic_cache.go # DynamicClientCache（按集群缓存 RESTMapper，避免重复 Discovery）
```

- [ ] **Step 4: 更新设计约定**

在"设计约定"小节末尾追加：

```markdown
- `list_resources` 是低频/新资源类型的统一入口，高频资源（Pod/Node/Deployment）保留专属工具以提供更精准的字段
- RESTMapper 按集群缓存，`describe_resource`/`apply_manifest`/`delete_resource` 首次调用后无额外 Discovery 开销
```

- [ ] **Step 5: 提交**

```bash
git add README.md
git commit -m "docs: 更新 README 工具列表，反映重构后的工具结构"
```

---

## 验收检查

- [ ] `go test ./...` 全部通过
- [ ] `go build ./...` 编译无错误
- [ ] `go vet ./...` 无告警
- [ ] `list_replicasets` 工具已删除
- [ ] `list_resources` 可正确处理 Service / Ingress / PVC / ConfigMap 等 kind
- [ ] CrashLoopBackOff 的 Pod 不再显示为 "Running"
- [ ] `describe_resource` 返回结果包含 annotations（不含 last-applied-configuration）
- [ ] `get_pod_logs` 支持 `previous=true` 参数
- [ ] README 工具列表与实际工具一致
