# K8s 多集群 MCP Server 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 开发一个支持多集群的 Kubernetes MCP Server，通过 stdio 接入 Claude Code，实现对多个 K8s 集群的统一查询和操作。

**Architecture:** 以 `ClusterManager` 作为核心，并发初始化多个 kubeconfig 对应的 k8s client；各 Tool 文件各自注册 MCP 工具，调用 `ClusterManager.Get()` 获取目标集群 client 后执行操作；所有返回值为结构化 JSON 摘要，避免原始对象噪音。

**Tech Stack:** Go 1.22+, `github.com/mark3labs/mcp-go`, `k8s.io/client-go`, `k8s.io/client-go/kubernetes/fake`（测试用）

---

## 文件结构

| 文件 | 职责 |
|------|------|
| `main.go` | 解析参数、初始化 ClusterManager、注册所有 Tools、启动 stdio MCP Server |
| `go.mod` / `go.sum` | 依赖管理 |
| `config/loader.go` | 扫描 kubeconfig 目录，返回 `map[clusterName]kubeconfigPath` |
| `config/loader_test.go` | loader 单元测试 |
| `k8s/manager.go` | ClusterManager 实现：并发初始化、Get、ListNames、Reload |
| `k8s/manager_test.go` | manager 单元测试（用 fake client） |
| `tools/types.go` | 所有 Tool 共享的返回结构体定义 |
| `tools/cluster.go` | `list_clusters` 工具注册 |
| `tools/namespace.go` | `list_namespaces` 工具注册 |
| `tools/node.go` | `list_nodes`, `describe_node` 工具注册 |
| `tools/pod.go` | `list_pods`, `get_pod_logs`, `describe_pod` 工具注册 |
| `tools/deployment.go` | `list_deployments`, `scale_deployment` 工具注册 |
| `tools/event.go` | `get_events` 工具注册 |
| `tools/resource.go` | `apply_manifest`, `delete_resource`, `describe_resource` 工具注册 |
| `tools/helpers.go` | 公共辅助函数：ageString、formatReady、toJSON |
| `CLAUDE.md` | 项目开发规范 |
| `Makefile` | 构建脚本 |

---

## Task 1: 初始化 Go 模块与依赖

**Files:**
- Create: `go.mod`

- [ ] **Step 1: 初始化 go module**

```bash
cd /Volumes/data/personal/code/k8s-mcp
go mod init github.com/yuanzhihao/k8s-mcp
```

- [ ] **Step 2: 添加依赖**

```bash
go get github.com/mark3labs/mcp-go@latest
go get k8s.io/client-go@v0.29.3
go get k8s.io/api@v0.29.3
go get k8s.io/apimachinery@v0.29.3
go get k8s.io/client-go/kubernetes/fake
```

- [ ] **Step 3: 确认 go.mod 内容正确**

```bash
cat go.mod
```

期望包含：
```
module github.com/yuanzhihao/k8s-mcp

go 1.22

require (
    github.com/mark3labs/mcp-go ...
    k8s.io/api v0.29.3
    k8s.io/apimachinery v0.29.3
    k8s.io/client-go v0.29.3
)
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: initialize go module with dependencies"
```

---

## Task 2: config/loader.go — kubeconfig 目录扫描

**Files:**
- Create: `config/loader.go`
- Create: `config/loader_test.go`

- [ ] **Step 1: 写失败测试**

创建 `config/loader_test.go`：

```go
package config

import (
    "os"
    "path/filepath"
    "testing"
)

func TestLoadKubeconfigs(t *testing.T) {
    // 创建临时目录，放两个假 yaml 文件
    dir := t.TempDir()
    for _, name := range []string{"prod-gpu.yaml", "dev-gpu.yaml"} {
        if err := os.WriteFile(filepath.Join(dir, name), []byte("fake"), 0600); err != nil {
            t.Fatal(err)
        }
    }
    // 放一个非 yaml 文件，不应被加载
    if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore"), 0600); err != nil {
        t.Fatal(err)
    }

    result, err := LoadKubeconfigs(dir)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(result) != 2 {
        t.Fatalf("expected 2 clusters, got %d", len(result))
    }
    if _, ok := result["prod-gpu"]; !ok {
        t.Error("expected cluster 'prod-gpu'")
    }
    if _, ok := result["dev-gpu"]; !ok {
        t.Error("expected cluster 'dev-gpu'")
    }
}

func TestLoadKubeconfigs_DirNotExist(t *testing.T) {
    _, err := LoadKubeconfigs("/nonexistent/path")
    if err == nil {
        t.Fatal("expected error for nonexistent directory")
    }
}
```

- [ ] **Step 2: 运行确认测试失败**

```bash
cd /Volumes/data/personal/code/k8s-mcp
go test ./config/... -v
```

期望：编译失败，`LoadKubeconfigs` 未定义

- [ ] **Step 3: 实现 config/loader.go**

```go
package config

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"
)

// LoadKubeconfigs 扫描指定目录下的 .yaml/.yml 文件，
// 返回 map[clusterName]absoluteFilePath
// 文件名（不含扩展名）即为集群名
func LoadKubeconfigs(dir string) (map[string]string, error) {
    expanded := expandHome(dir)
    entries, err := os.ReadDir(expanded)
    if err != nil {
        return nil, fmt.Errorf("读取 kubeconfig 目录失败 %q: %w", dir, err)
    }

    result := make(map[string]string)
    for _, e := range entries {
        if e.IsDir() {
            continue
        }
        name := e.Name()
        ext := strings.ToLower(filepath.Ext(name))
        if ext != ".yaml" && ext != ".yml" {
            continue
        }
        clusterName := strings.TrimSuffix(name, filepath.Ext(name))
        result[clusterName] = filepath.Join(expanded, name)
    }
    return result, nil
}

func expandHome(path string) string {
    if strings.HasPrefix(path, "~/") {
        home, err := os.UserHomeDir()
        if err != nil {
            return path
        }
        return filepath.Join(home, path[2:])
    }
    return path
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./config/... -v
```

期望：`PASS`，两个测试用例均通过

- [ ] **Step 5: Commit**

```bash
git add config/
git commit -m "feat: add kubeconfig directory loader"
```

---

## Task 3: k8s/manager.go — 多集群连接管理器

**Files:**
- Create: `k8s/manager.go`
- Create: `k8s/manager_test.go`

- [ ] **Step 1: 写失败测试**

创建 `k8s/manager_test.go`：

```go
package k8s

import (
    "sort"
    "testing"

    "k8s.io/client-go/kubernetes/fake"
)

// newManagerWithFakes 直接注入 fake client，绕过真实 kubeconfig
func newManagerWithFakes(clients map[string]kubernetesClient) *ClusterManager {
    m := &ClusterManager{
        clients: clients,
        errors:  make(map[string]error),
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
    if !containsStr(err.Error(), "prod") {
        t.Errorf("error should mention available clusters, got: %v", err)
    }
}

func containsStr(s, sub string) bool {
    return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
    for i := 0; i <= len(s)-len(sub); i++ {
        if s[i:i+len(sub)] == sub {
            return true
        }
    }
    return false
}
```

- [ ] **Step 2: 运行确认测试失败**

```bash
go test ./k8s/... -v
```

期望：编译失败

- [ ] **Step 3: 实现 k8s/manager.go**

```go
package k8s

import (
    "fmt"
    "sort"
    "strings"
    "sync"

    "github.com/yuanzhihao/k8s-mcp/config"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
)

// kubernetesClient 接口，便于测试时注入 fake
type kubernetesClient interface {
    kubernetes.Interface
}

// ClusterManager 管理多集群 k8s client
type ClusterManager struct {
    clients map[string]kubernetesClient
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
        clients: make(map[string]kubernetesClient),
        errors:  make(map[string]error),
    }

    type result struct {
        name   string
        client kubernetesClient
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
            ch <- result{name: n, client: client}
        }(name, path)
    }

    for range kubeconfigs {
        r := <-ch
        if r.err != nil {
            m.errors[r.name] = r.err
        } else {
            m.clients[r.name] = r.client
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
        available := m.ListNames()
        sort.Strings(available)
        return nil, fmt.Errorf("集群 %q 不存在，可用集群: [%s]", clusterName, strings.Join(available, ", "))
    }
    return client, nil
}

// ListNames 返回所有已成功连接的集群名列表
func (m *ClusterManager) ListNames() []string {
    m.mu.RLock()
    defer m.mu.RUnlock()

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
    m.errors = newMgr.errors
    return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./k8s/... -v
```

期望：3 个测试全部 `PASS`

- [ ] **Step 5: Commit**

```bash
git add k8s/
git commit -m "feat: add multi-cluster manager with concurrent initialization"
```

---

## Task 4: tools/types.go — 共享返回结构体

**Files:**
- Create: `tools/types.go`

- [ ] **Step 1: 创建 tools/types.go**

```go
package tools

// ClusterInfo 单个集群状态
type ClusterInfo struct {
    Name   string `json:"name"`
    Status string `json:"status"` // "ok" | "error"
    Error  string `json:"error,omitempty"`
}

// ClusterListResult list_clusters 返回值
type ClusterListResult struct {
    Total  int           `json:"total"`
    Items  []ClusterInfo `json:"items"`
}

// NamespaceBrief 命名空间摘要
type NamespaceBrief struct {
    Name   string `json:"name"`
    Status string `json:"status"`
    Age    string `json:"age"`
}

// NamespaceListResult list_namespaces 返回值
type NamespaceListResult struct {
    Cluster string           `json:"cluster"`
    Total   int              `json:"total"`
    Items   []NamespaceBrief `json:"items"`
}

// NodeBrief 节点摘要
type NodeBrief struct {
    Name     string `json:"name"`
    Status   string `json:"status"`
    Roles    string `json:"roles"`
    Age      string `json:"age"`
    Version  string `json:"version"`
    CPUUsage string `json:"cpu_usage,omitempty"`
    MemUsage string `json:"mem_usage,omitempty"`
}

// NodeListResult list_nodes 返回值
type NodeListResult struct {
    Cluster string      `json:"cluster"`
    Total   int         `json:"total"`
    Items   []NodeBrief `json:"items"`
}

// NodeDetail describe_node 返回值
type NodeDetail struct {
    Cluster     string            `json:"cluster"`
    Name        string            `json:"name"`
    Status      string            `json:"status"`
    Roles       string            `json:"roles"`
    Age         string            `json:"age"`
    Version     string            `json:"version"`
    OS          string            `json:"os"`
    Arch        string            `json:"arch"`
    Addresses   []string          `json:"addresses"`
    Capacity    map[string]string `json:"capacity"`
    Allocatable map[string]string `json:"allocatable"`
    Conditions  []string          `json:"conditions"`
}

// PodBrief Pod 摘要
type PodBrief struct {
    Name      string `json:"name"`
    Namespace string `json:"namespace"`
    Status    string `json:"status"`
    Ready     string `json:"ready"`     // "2/3"
    Restarts  int32  `json:"restarts"`
    Age       string `json:"age"`
    NodeName  string `json:"node_name"`
}

// PodListResult list_pods 返回值
type PodListResult struct {
    Cluster   string     `json:"cluster"`
    Namespace string     `json:"namespace"`
    Total     int        `json:"total"`
    Items     []PodBrief `json:"items"`
}

// PodDetail describe_pod 返回值
type PodDetail struct {
    Cluster    string            `json:"cluster"`
    Namespace  string            `json:"namespace"`
    Name       string            `json:"name"`
    Status     string            `json:"status"`
    NodeName   string            `json:"node_name"`
    Age        string            `json:"age"`
    Labels     map[string]string `json:"labels,omitempty"`
    Containers []ContainerInfo   `json:"containers"`
    Conditions []string          `json:"conditions"`
}

// ContainerInfo 容器摘要
type ContainerInfo struct {
    Name     string `json:"name"`
    Image    string `json:"image"`
    Ready    bool   `json:"ready"`
    Restarts int32  `json:"restarts"`
    State    string `json:"state"`
}

// DeploymentBrief Deployment 摘要
type DeploymentBrief struct {
    Name      string `json:"name"`
    Namespace string `json:"namespace"`
    Ready     string `json:"ready"`   // "2/3"
    UpToDate  int32  `json:"up_to_date"`
    Available int32  `json:"available"`
    Age       string `json:"age"`
}

// DeploymentListResult list_deployments 返回值
type DeploymentListResult struct {
    Cluster   string            `json:"cluster"`
    Namespace string            `json:"namespace"`
    Total     int               `json:"total"`
    Items     []DeploymentBrief `json:"items"`
}

// EventBrief 事件摘要
type EventBrief struct {
    Namespace string `json:"namespace"`
    Kind      string `json:"kind"`
    Name      string `json:"name"`
    Reason    string `json:"reason"`
    Message   string `json:"message"`
    Type      string `json:"type"` // Normal | Warning
    Count     int32  `json:"count"`
    Age       string `json:"age"`
}

// EventListResult get_events 返回值
type EventListResult struct {
    Cluster   string       `json:"cluster"`
    Namespace string       `json:"namespace"`
    Total     int          `json:"total"`
    Items     []EventBrief `json:"items"`
}

// ResourceDetail describe_resource 返回值
type ResourceDetail struct {
    Cluster   string                 `json:"cluster"`
    Kind      string                 `json:"kind"`
    Namespace string                 `json:"namespace"`
    Name      string                 `json:"name"`
    Age       string                 `json:"age"`
    Labels    map[string]string      `json:"labels,omitempty"`
    Spec      map[string]interface{} `json:"spec,omitempty"`
    Status    map[string]interface{} `json:"status,omitempty"`
}
```

- [ ] **Step 2: 确认编译通过**

```bash
go build ./tools/...
```

- [ ] **Step 3: Commit**

```bash
git add tools/types.go
git commit -m "feat: add shared result types for all tools"
```

---

## Task 5: tools/helpers.go — 公共辅助函数

**Files:**
- Create: `tools/helpers.go`
- Create: `tools/helpers_test.go`

- [ ] **Step 1: 写失败测试**

创建 `tools/helpers_test.go`：

```go
package tools

import (
    "encoding/json"
    "testing"
    "time"
)

func TestAgeString(t *testing.T) {
    cases := []struct {
        dur      time.Duration
        expected string
    }{
        {30 * time.Second, "30s"},
        {5 * time.Minute, "5m"},
        {3 * time.Hour, "3h"},
        {48 * time.Hour, "2d"},
    }
    for _, c := range cases {
        t.Run(c.expected, func(t *testing.T) {
            got := ageString(time.Now().Add(-c.dur))
            if got != c.expected {
                t.Errorf("expected %q, got %q", c.expected, got)
            }
        })
    }
}

func TestToJSON(t *testing.T) {
    type Sample struct {
        Name string `json:"name"`
    }
    result := toJSON(Sample{Name: "test"})
    var m map[string]interface{}
    if err := json.Unmarshal([]byte(result), &m); err != nil {
        t.Fatalf("toJSON produced invalid JSON: %v", err)
    }
    if m["name"] != "test" {
        t.Errorf("unexpected value: %v", m)
    }
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./tools/... -run TestAgeString -v
```

期望：编译失败

- [ ] **Step 3: 实现 tools/helpers.go**

```go
package tools

import (
    "encoding/json"
    "fmt"
    "time"
)

// ageString 将时间点转换为人类可读的相对时间字符串
func ageString(t time.Time) string {
    d := time.Since(t)
    switch {
    case d < time.Minute:
        return fmt.Sprintf("%ds", int(d.Seconds()))
    case d < time.Hour:
        return fmt.Sprintf("%dm", int(d.Minutes()))
    case d < 24*time.Hour:
        return fmt.Sprintf("%dh", int(d.Hours()))
    default:
        return fmt.Sprintf("%dd", int(d.Hours()/24))
    }
}

// toJSON 将任意结构体序列化为 JSON 字符串，序列化失败时返回错误描述
func toJSON(v interface{}) string {
    b, err := json.Marshal(v)
    if err != nil {
        return fmt.Sprintf(`{"error":"json marshal failed: %s"}`, err.Error())
    }
    return string(b)
}

// toolError 生成标准错误 JSON
func toolError(msg string) string {
    return fmt.Sprintf(`{"error":%q}`, msg)
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./tools/... -v
```

- [ ] **Step 5: Commit**

```bash
git add tools/helpers.go tools/helpers_test.go
git commit -m "feat: add shared helper functions for tools"
```

---

## Task 6: tools/cluster.go — list_clusters 工具

**Files:**
- Create: `tools/cluster.go`
- Create: `tools/cluster_test.go`

- [ ] **Step 1: 了解 mcp-go API**

```bash
grep -r "AddTool\|NewTool\|Tool{" $(go env GOPATH)/pkg/mod/github.com/mark3labs/mcp-go*/server/ 2>/dev/null | head -30
```

然后查看：
```bash
cat $(go env GOPATH)/pkg/mod/github.com/mark3labs/mcp-go*/mcp/types.go 2>/dev/null | head -80
```

- [ ] **Step 2: 写失败测试**

创建 `tools/cluster_test.go`：

```go
package tools

import (
    "encoding/json"
    "testing"

    "k8s.io/client-go/kubernetes/fake"
)

// mockManager 用于测试的假 ClusterManager 接口
type mockManager struct {
    names  []string
    errors map[string]string
}

func (m *mockManager) ListNames() []string        { return m.names }
func (m *mockManager) ListErrors() map[string]string { return m.errors }
func (m *mockManager) Get(name string) (interface{}, error) {
    return fake.NewSimpleClientset(), nil
}

func TestBuildClusterList(t *testing.T) {
    mgr := &mockManager{
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
```

- [ ] **Step 3: 运行确认失败**

```bash
go test ./tools/... -run TestBuildClusterList -v
```

- [ ] **Step 4: 实现 tools/cluster.go**

```go
package tools

import (
    "context"
    "sort"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    "k8s.io/client-go/kubernetes"
)

// ClusterManagerInterface 供 tools 包调用的集群管理接口
type ClusterManagerInterface interface {
    Get(clusterName string) (kubernetes.Interface, error)
    ListNames() []string
    ListErrors() map[string]string
}

// RegisterClusterTools 注册集群相关 MCP 工具
func RegisterClusterTools(s *server.MCPServer, mgr ClusterManagerInterface) {
    s.AddTool(mcp.NewTool("list_clusters",
        mcp.WithDescription("列出所有已加载的 K8s 集群及其连接状态"),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        return mcp.NewToolResultText(buildClusterList(mgr)), nil
    })
}

type clusterLister interface {
    ListNames() []string
    ListErrors() map[string]string
}

func buildClusterList(mgr clusterLister) string {
    names := mgr.ListNames()
    errs := mgr.ListErrors()

    sort.Strings(names)

    items := make([]ClusterInfo, 0, len(names)+len(errs))
    for _, name := range names {
        items = append(items, ClusterInfo{Name: name, Status: "ok"})
    }

    errNames := make([]string, 0, len(errs))
    for n := range errs {
        errNames = append(errNames, n)
    }
    sort.Strings(errNames)
    for _, name := range errNames {
        items = append(items, ClusterInfo{Name: name, Status: "error", Error: errs[name]})
    }

    return toJSON(ClusterListResult{
        Total: len(items),
        Items: items,
    })
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
go test ./tools/... -run TestBuildClusterList -v
```

- [ ] **Step 6: Commit**

```bash
git add tools/cluster.go tools/cluster_test.go
git commit -m "feat: add list_clusters tool"
```

---

## Task 7: main.go — MCP Server 入口与 list_pods 初步接入

**Files:**
- Create: `main.go`
- Create: `tools/pod.go`
- Create: `tools/pod_test.go`

- [ ] **Step 1: 写 list_pods 失败测试**

创建 `tools/pod_test.go`：

```go
package tools

import (
    "encoding/json"
    "testing"
    "time"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes/fake"
)

func makePod(name, ns, phase string, ready, total int32, restarts int32, nodeName string) corev1.Pod {
    conditions := []corev1.ContainerStatus{}
    for i := int32(0); i < total; i++ {
        conditions = append(conditions, corev1.ContainerStatus{
            Ready:            i < ready,
            RestartCount:     restarts,
            Name:             fmt.Sprintf("container-%d", i),
        })
    }
    return corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name:              name,
            Namespace:         ns,
            CreationTimestamp: metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
        },
        Spec: corev1.PodSpec{NodeName: nodeName},
        Status: corev1.PodStatus{
            Phase:             corev1.PodPhase(phase),
            ContainerStatuses: conditions,
        },
    }
}

func TestListPodsHandler(t *testing.T) {
    pod1 := makePod("web-1", "default", "Running", 1, 1, 0, "node-1")
    pod2 := makePod("web-2", "default", "Pending", 0, 1, 2, "")

    fc := fake.NewSimpleClientset(&pod1, &pod2)

    result := listPodsImpl(fc, "prod", "default", "", 20)

    var r PodListResult
    if err := json.Unmarshal([]byte(result), &r); err != nil {
        t.Fatalf("invalid JSON: %v\n%s", err, result)
    }
    if r.Cluster != "prod" {
        t.Errorf("expected cluster=prod, got %s", r.Cluster)
    }
    if r.Total != 2 {
        t.Errorf("expected total=2, got %d", r.Total)
    }
}
```

注意：需要在文件顶部加 `import "fmt"`。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./tools/... -run TestListPodsHandler -v
```

- [ ] **Step 3: 实现 tools/pod.go（含 list_pods）**

```go
package tools

import (
    "context"
    "fmt"
    "strconv"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

// RegisterPodTools 注册 Pod 相关 MCP 工具
func RegisterPodTools(s *server.MCPServer, mgr ClusterManagerInterface) {
    // list_pods
    s.AddTool(mcp.NewTool("list_pods",
        mcp.WithDescription("列出指定集群和命名空间下的 Pod 摘要"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间，使用 '' 表示所有命名空间")),
        mcp.WithString("label_selector", mcp.Description("标签选择器，例如 app=nginx")),
        mcp.WithNumber("limit", mcp.Description("返回数量上限，默认 20")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        namespace := req.Params.Arguments["namespace"].(string)
        labelSelector, _ := req.Params.Arguments["label_selector"].(string)
        limit := 20
        if l, ok := req.Params.Arguments["limit"].(float64); ok && l > 0 {
            limit = int(l)
        }

        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(listPodsImpl(client, cluster, namespace, labelSelector, limit)), nil
    })

    // get_pod_logs
    s.AddTool(mcp.NewTool("get_pod_logs",
        mcp.WithDescription("获取指定 Pod 的容器日志"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
        mcp.WithString("pod", mcp.Required(), mcp.Description("Pod 名称")),
        mcp.WithString("container", mcp.Description("容器名称，多容器时必填")),
        mcp.WithNumber("tail", mcp.Description("返回最后 N 行，默认 100")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        namespace := req.Params.Arguments["namespace"].(string)
        pod := req.Params.Arguments["pod"].(string)
        container, _ := req.Params.Arguments["container"].(string)
        tail := int64(100)
        if t, ok := req.Params.Arguments["tail"].(float64); ok && t > 0 {
            tail = int64(t)
        }

        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(getPodLogsImpl(ctx, client, cluster, namespace, pod, container, tail)), nil
    })

    // describe_pod
    s.AddTool(mcp.NewTool("describe_pod",
        mcp.WithDescription("获取指定 Pod 的详细信息"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
        mcp.WithString("name", mcp.Required(), mcp.Description("Pod 名称")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        namespace := req.Params.Arguments["namespace"].(string)
        name := req.Params.Arguments["name"].(string)

        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(describePodImpl(ctx, client, cluster, namespace, name)), nil
    })
}

func listPodsImpl(client kubernetes.Interface, cluster, namespace, labelSelector string, limit int) string {
    listOpts := metav1.ListOptions{
        LabelSelector: labelSelector,
        Limit:         int64(limit),
    }
    pods, err := client.CoreV1().Pods(namespace).List(context.Background(), listOpts)
    if err != nil {
        return toolError(fmt.Sprintf("list pods 失败: %v", err))
    }

    items := make([]PodBrief, 0, len(pods.Items))
    for _, p := range pods.Items {
        ready, total := countReady(p)
        restarts := countRestarts(p)
        items = append(items, PodBrief{
            Name:      p.Name,
            Namespace: p.Namespace,
            Status:    string(p.Status.Phase),
            Ready:     fmt.Sprintf("%d/%d", ready, total),
            Restarts:  restarts,
            Age:       ageString(p.CreationTimestamp.Time),
            NodeName:  p.Spec.NodeName,
        })
    }

    return toJSON(PodListResult{
        Cluster:   cluster,
        Namespace: namespace,
        Total:     len(items),
        Items:     items,
    })
}

func getPodLogsImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace, pod, container string, tail int64) string {
    opts := &corev1PodLogOptions{TailLines: &tail}
    if container != "" {
        opts.Container = container
    }
    req := client.CoreV1().Pods(namespace).GetLogs(pod, (*corev1.PodLogOptions)(opts))
    logs, err := req.DoRaw(ctx)
    if err != nil {
        return toolError(fmt.Sprintf("获取日志失败: %v", err))
    }
    return toJSON(map[string]interface{}{
        "cluster":   cluster,
        "namespace": namespace,
        "pod":       pod,
        "container": container,
        "tail":      tail,
        "logs":      string(logs),
    })
}

func describePodImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace, name string) string {
    p, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return toolError(fmt.Sprintf("get pod 失败: %v", err))
    }

    containers := make([]ContainerInfo, 0, len(p.Status.ContainerStatuses))
    for _, cs := range p.Status.ContainerStatuses {
        state := containerState(cs)
        containers = append(containers, ContainerInfo{
            Name:     cs.Name,
            Image:    cs.Image,
            Ready:    cs.Ready,
            Restarts: cs.RestartCount,
            State:    state,
        })
    }

    conditions := make([]string, 0, len(p.Status.Conditions))
    for _, c := range p.Status.Conditions {
        conditions = append(conditions, fmt.Sprintf("%s=%s", c.Type, c.Status))
    }

    return toJSON(PodDetail{
        Cluster:    cluster,
        Namespace:  namespace,
        Name:       p.Name,
        Status:     string(p.Status.Phase),
        NodeName:   p.Spec.NodeName,
        Age:        ageString(p.CreationTimestamp.Time),
        Labels:     p.Labels,
        Containers: containers,
        Conditions: conditions,
    })
}

func countReady(p corev1.Pod) (ready, total int32) {
    for _, cs := range p.Status.ContainerStatuses {
        total++
        if cs.Ready {
            ready++
        }
    }
    return
}

func countRestarts(p corev1.Pod) int32 {
    var total int32
    for _, cs := range p.Status.ContainerStatuses {
        total += cs.RestartCount
    }
    return total
}

func containerState(cs corev1.ContainerStatus) string {
    if cs.State.Running != nil {
        return "Running"
    }
    if cs.State.Waiting != nil {
        return "Waiting: " + cs.State.Waiting.Reason
    }
    if cs.State.Terminated != nil {
        return "Terminated: " + cs.State.Terminated.Reason
    }
    return "Unknown"
}

// corev1PodLogOptions 别名，避免循环导入时的类型转换问题
type corev1PodLogOptions = corev1.PodLogOptions
```

注意：需要在顶部添加 `corev1 "k8s.io/api/core/v1"` import。

- [ ] **Step 4: 修正 pod.go 的 import**

在 `tools/pod.go` 顶部 import 中添加：
```go
corev1 "k8s.io/api/core/v1"
```

以及去掉 `strconv` 如果未使用。

- [ ] **Step 5: 创建 main.go**

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
    kubeconfigDir := flag.String("kubeconfig-dir", "~/.kube/clusters", "kubeconfig 文件目录")
    flag.Parse()

    mgr, err := k8s.NewClusterManager(*kubeconfigDir)
    if err != nil {
        log.Fatalf("初始化集群管理器失败: %v", err)
    }

    errs := mgr.ListErrors()
    for name, errMsg := range errs {
        log.Printf("警告: 集群 %q 连接失败: %s", name, errMsg)
    }

    s := server.NewMCPServer("k8s-mcp", "1.0.0",
        server.WithToolCapabilities(true),
    )

    tools.RegisterClusterTools(s, mgr)
    tools.RegisterPodTools(s, mgr)

    log.Printf("k8s-mcp server 启动，已加载集群: %v", mgr.ListNames())

    if err := server.ServeStdio(s); err != nil {
        log.Fatalf("MCP Server 异常退出: %v", err)
    }
}
```

- [ ] **Step 6: 编译确认无错误**

```bash
go build ./...
```

- [ ] **Step 7: 运行 pod 测试**

```bash
go test ./tools/... -run TestListPodsHandler -v
```

- [ ] **Step 8: Commit**

```bash
git add main.go tools/pod.go tools/pod_test.go
git commit -m "feat: add list_pods, get_pod_logs, describe_pod tools and main entry"
```

---

## Task 8: tools/namespace.go — list_namespaces

**Files:**
- Create: `tools/namespace.go`
- Create: `tools/namespace_test.go`

- [ ] **Step 1: 写失败测试**

创建 `tools/namespace_test.go`：

```go
package tools

import (
    "encoding/json"
    "testing"
    "time"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes/fake"
)

func TestListNamespacesImpl(t *testing.T) {
    ns1 := corev1.Namespace{
        ObjectMeta: metav1.ObjectMeta{
            Name:              "default",
            CreationTimestamp: metav1.Time{Time: time.Now().Add(-24 * time.Hour)},
        },
        Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
    }
    ns2 := corev1.Namespace{
        ObjectMeta: metav1.ObjectMeta{
            Name:              "kube-system",
            CreationTimestamp: metav1.Time{Time: time.Now().Add(-48 * time.Hour)},
        },
        Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
    }

    fc := fake.NewSimpleClientset(&ns1, &ns2)
    result := listNamespacesImpl(fc, "prod")

    var r NamespaceListResult
    if err := json.Unmarshal([]byte(result), &r); err != nil {
        t.Fatalf("invalid JSON: %v\n%s", err, result)
    }
    if r.Cluster != "prod" {
        t.Errorf("expected cluster=prod, got %s", r.Cluster)
    }
    if r.Total != 2 {
        t.Errorf("expected total=2, got %d", r.Total)
    }
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./tools/... -run TestListNamespacesImpl -v
```

- [ ] **Step 3: 实现 tools/namespace.go**

```go
package tools

import (
    "context"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

// RegisterNamespaceTools 注册命名空间相关 MCP 工具
func RegisterNamespaceTools(s *server.MCPServer, mgr ClusterManagerInterface) {
    s.AddTool(mcp.NewTool("list_namespaces",
        mcp.WithDescription("列出指定集群的所有命名空间"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(listNamespacesImpl(client, cluster)), nil
    })
}

func listNamespacesImpl(client kubernetes.Interface, cluster string) string {
    nsList, err := client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
    if err != nil {
        return toolError(fmt.Sprintf("list namespaces 失败: %v", err))
    }

    items := make([]NamespaceBrief, 0, len(nsList.Items))
    for _, ns := range nsList.Items {
        items = append(items, NamespaceBrief{
            Name:   ns.Name,
            Status: string(ns.Status.Phase),
            Age:    ageString(ns.CreationTimestamp.Time),
        })
    }

    return toJSON(NamespaceListResult{
        Cluster: cluster,
        Total:   len(items),
        Items:   items,
    })
}
```

- [ ] **Step 4: 在 main.go 中注册 namespace tools**

在 `main.go` 的 `tools.RegisterPodTools(s, mgr)` 后添加：
```go
tools.RegisterNamespaceTools(s, mgr)
```

- [ ] **Step 5: 运行测试**

```bash
go test ./tools/... -run TestListNamespacesImpl -v
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add tools/namespace.go tools/namespace_test.go main.go
git commit -m "feat: add list_namespaces tool"
```

---

## Task 9: tools/node.go — list_nodes / describe_node

**Files:**
- Create: `tools/node.go`
- Create: `tools/node_test.go`

- [ ] **Step 1: 写失败测试**

创建 `tools/node_test.go`：

```go
package tools

import (
    "encoding/json"
    "testing"
    "time"

    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/resource"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes/fake"
)

func makeNode(name string, labels map[string]string) corev1.Node {
    return corev1.Node{
        ObjectMeta: metav1.ObjectMeta{
            Name:              name,
            Labels:            labels,
            CreationTimestamp: metav1.Time{Time: time.Now().Add(-72 * time.Hour)},
        },
        Status: corev1.NodeStatus{
            Conditions: []corev1.NodeCondition{
                {Type: corev1.NodeReady, Status: corev1.ConditionTrue},
            },
            NodeInfo: corev1.NodeSystemInfo{
                KubeletVersion:          "v1.29.0",
                OperatingSystem:         "linux",
                Architecture:            "amd64",
            },
            Capacity: corev1.ResourceList{
                corev1.ResourceCPU:    resource.MustParse("8"),
                corev1.ResourceMemory: resource.MustParse("32Gi"),
            },
            Addresses: []corev1.NodeAddress{
                {Type: corev1.NodeInternalIP, Address: "192.168.1.10"},
            },
        },
    }
}

func TestListNodesImpl(t *testing.T) {
    node := makeNode("worker-1", map[string]string{"node-role.kubernetes.io/worker": ""})
    fc := fake.NewSimpleClientset(&node)

    result := listNodesImpl(fc, "prod")

    var r NodeListResult
    if err := json.Unmarshal([]byte(result), &r); err != nil {
        t.Fatalf("invalid JSON: %v\n%s", err, result)
    }
    if r.Total != 1 {
        t.Errorf("expected 1 node, got %d", r.Total)
    }
    if r.Items[0].Status != "Ready" {
        t.Errorf("expected status=Ready, got %s", r.Items[0].Status)
    }
}

func TestDescribeNodeImpl(t *testing.T) {
    node := makeNode("master-1", map[string]string{"node-role.kubernetes.io/control-plane": ""})
    fc := fake.NewSimpleClientset(&node)

    result := describeNodeImpl(fc, "prod", "master-1")

    var r NodeDetail
    if err := json.Unmarshal([]byte(result), &r); err != nil {
        t.Fatalf("invalid JSON: %v\n%s", err, result)
    }
    if r.Name != "master-1" {
        t.Errorf("expected name=master-1, got %s", r.Name)
    }
    if r.Roles != "control-plane" {
        t.Errorf("expected roles=control-plane, got %s", r.Roles)
    }
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./tools/... -run "TestListNodesImpl|TestDescribeNodeImpl" -v
```

- [ ] **Step 3: 实现 tools/node.go**

```go
package tools

import (
    "context"
    "fmt"
    "strings"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

// RegisterNodeTools 注册节点相关 MCP 工具
func RegisterNodeTools(s *server.MCPServer, mgr ClusterManagerInterface) {
    s.AddTool(mcp.NewTool("list_nodes",
        mcp.WithDescription("列出指定集群的所有节点及状态"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(listNodesImpl(client, cluster)), nil
    })

    s.AddTool(mcp.NewTool("describe_node",
        mcp.WithDescription("获取指定节点的详细信息"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("name", mcp.Required(), mcp.Description("节点名称")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        name := req.Params.Arguments["name"].(string)
        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(describeNodeImpl(client, cluster, name)), nil
    })
}

func listNodesImpl(client kubernetes.Interface, cluster string) string {
    nodes, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
    if err != nil {
        return toolError(fmt.Sprintf("list nodes 失败: %v", err))
    }

    items := make([]NodeBrief, 0, len(nodes.Items))
    for _, n := range nodes.Items {
        items = append(items, NodeBrief{
            Name:    n.Name,
            Status:  nodeStatus(n),
            Roles:   nodeRoles(n),
            Age:     ageString(n.CreationTimestamp.Time),
            Version: n.Status.NodeInfo.KubeletVersion,
        })
    }

    return toJSON(NodeListResult{
        Cluster: cluster,
        Total:   len(items),
        Items:   items,
    })
}

func describeNodeImpl(client kubernetes.Interface, cluster, name string) string {
    n, err := client.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
    if err != nil {
        return toolError(fmt.Sprintf("get node 失败: %v", err))
    }

    addresses := make([]string, 0, len(n.Status.Addresses))
    for _, addr := range n.Status.Addresses {
        addresses = append(addresses, fmt.Sprintf("%s=%s", addr.Type, addr.Address))
    }

    capacity := make(map[string]string)
    for res, qty := range n.Status.Capacity {
        capacity[string(res)] = qty.String()
    }

    allocatable := make(map[string]string)
    for res, qty := range n.Status.Allocatable {
        allocatable[string(res)] = qty.String()
    }

    conditions := make([]string, 0, len(n.Status.Conditions))
    for _, c := range n.Status.Conditions {
        conditions = append(conditions, fmt.Sprintf("%s=%s", c.Type, c.Status))
    }

    return toJSON(NodeDetail{
        Cluster:     cluster,
        Name:        n.Name,
        Status:      nodeStatus(*n),
        Roles:       nodeRoles(*n),
        Age:         ageString(n.CreationTimestamp.Time),
        Version:     n.Status.NodeInfo.KubeletVersion,
        OS:          n.Status.NodeInfo.OperatingSystem,
        Arch:        n.Status.NodeInfo.Architecture,
        Addresses:   addresses,
        Capacity:    capacity,
        Allocatable: allocatable,
        Conditions:  conditions,
    })
}

func nodeStatus(n corev1.Node) string {
    for _, c := range n.Status.Conditions {
        if c.Type == corev1.NodeReady {
            if c.Status == corev1.ConditionTrue {
                return "Ready"
            }
            return "NotReady"
        }
    }
    return "Unknown"
}

func nodeRoles(n corev1.Node) string {
    roles := []string{}
    for label := range n.Labels {
        if strings.HasPrefix(label, "node-role.kubernetes.io/") {
            role := strings.TrimPrefix(label, "node-role.kubernetes.io/")
            if role != "" {
                roles = append(roles, role)
            }
        }
    }
    if len(roles) == 0 {
        return "<none>"
    }
    return strings.Join(roles, ",")
}
```

- [ ] **Step 4: 在 main.go 中注册 node tools**

在 `tools.RegisterNamespaceTools(s, mgr)` 后添加：
```go
tools.RegisterNodeTools(s, mgr)
```

- [ ] **Step 5: 运行测试并编译**

```bash
go test ./tools/... -run "TestListNodesImpl|TestDescribeNodeImpl" -v
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add tools/node.go tools/node_test.go main.go
git commit -m "feat: add list_nodes and describe_node tools"
```

---

## Task 10: tools/deployment.go — list_deployments / scale_deployment

**Files:**
- Create: `tools/deployment.go`
- Create: `tools/deployment_test.go`

- [ ] **Step 1: 写失败测试**

创建 `tools/deployment_test.go`：

```go
package tools

import (
    "encoding/json"
    "testing"
    "time"

    appsv1 "k8s.io/api/apps/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes/fake"
)

func makeDeployment(name, ns string, desired, ready int32) appsv1.Deployment {
    return appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:              name,
            Namespace:         ns,
            CreationTimestamp: metav1.Time{Time: time.Now().Add(-2 * time.Hour)},
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: &desired,
        },
        Status: appsv1.DeploymentStatus{
            ReadyReplicas:     ready,
            AvailableReplicas: ready,
            UpdatedReplicas:   desired,
        },
    }
}

func TestListDeploymentsImpl(t *testing.T) {
    d1 := makeDeployment("nginx", "default", 3, 3)
    d2 := makeDeployment("api", "default", 2, 1)
    fc := fake.NewSimpleClientset(&d1, &d2)

    result := listDeploymentsImpl(fc, "prod", "default")

    var r DeploymentListResult
    if err := json.Unmarshal([]byte(result), &r); err != nil {
        t.Fatalf("invalid JSON: %v\n%s", err, result)
    }
    if r.Total != 2 {
        t.Errorf("expected 2, got %d", r.Total)
    }
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./tools/... -run TestListDeploymentsImpl -v
```

- [ ] **Step 3: 实现 tools/deployment.go**

```go
package tools

import (
    "context"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

// RegisterDeploymentTools 注册 Deployment 相关 MCP 工具
func RegisterDeploymentTools(s *server.MCPServer, mgr ClusterManagerInterface) {
    s.AddTool(mcp.NewTool("list_deployments",
        mcp.WithDescription("列出指定集群和命名空间的 Deployment"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        namespace := req.Params.Arguments["namespace"].(string)
        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(listDeploymentsImpl(client, cluster, namespace)), nil
    })

    s.AddTool(mcp.NewTool("scale_deployment",
        mcp.WithDescription("调整指定 Deployment 的副本数"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
        mcp.WithString("name", mcp.Required(), mcp.Description("Deployment 名称")),
        mcp.WithNumber("replicas", mcp.Required(), mcp.Description("目标副本数")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        namespace := req.Params.Arguments["namespace"].(string)
        name := req.Params.Arguments["name"].(string)
        replicas := int32(req.Params.Arguments["replicas"].(float64))
        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(scaleDeploymentImpl(ctx, client, cluster, namespace, name, replicas)), nil
    })
}

func listDeploymentsImpl(client kubernetes.Interface, cluster, namespace string) string {
    deps, err := client.AppsV1().Deployments(namespace).List(context.Background(), metav1.ListOptions{})
    if err != nil {
        return toolError(fmt.Sprintf("list deployments 失败: %v", err))
    }

    items := make([]DeploymentBrief, 0, len(deps.Items))
    for _, d := range deps.Items {
        desired := int32(0)
        if d.Spec.Replicas != nil {
            desired = *d.Spec.Replicas
        }
        items = append(items, DeploymentBrief{
            Name:      d.Name,
            Namespace: d.Namespace,
            Ready:     fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, desired),
            UpToDate:  d.Status.UpdatedReplicas,
            Available: d.Status.AvailableReplicas,
            Age:       ageString(d.CreationTimestamp.Time),
        })
    }

    return toJSON(DeploymentListResult{
        Cluster:   cluster,
        Namespace: namespace,
        Total:     len(items),
        Items:     items,
    })
}

func scaleDeploymentImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace, name string, replicas int32) string {
    scale, err := client.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
    if err != nil {
        return toolError(fmt.Sprintf("获取 scale 失败: %v", err))
    }

    scale.Spec.Replicas = replicas
    _, err = client.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
    if err != nil {
        return toolError(fmt.Sprintf("scale deployment 失败: %v", err))
    }

    return toJSON(map[string]interface{}{
        "cluster":   cluster,
        "namespace": namespace,
        "name":      name,
        "replicas":  replicas,
        "status":    "ok",
    })
}
```

- [ ] **Step 4: 在 main.go 中注册**

添加：
```go
tools.RegisterDeploymentTools(s, mgr)
```

- [ ] **Step 5: 运行测试**

```bash
go test ./tools/... -run TestListDeploymentsImpl -v
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add tools/deployment.go tools/deployment_test.go main.go
git commit -m "feat: add list_deployments and scale_deployment tools"
```

---

## Task 11: tools/event.go — get_events

**Files:**
- Create: `tools/event.go`
- Create: `tools/event_test.go`

- [ ] **Step 1: 写失败测试**

创建 `tools/event_test.go`：

```go
package tools

import (
    "encoding/json"
    "testing"
    "time"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes/fake"
)

func TestGetEventsImpl(t *testing.T) {
    ev := corev1.Event{
        ObjectMeta: metav1.ObjectMeta{
            Name:              "pod-crash.1234",
            Namespace:         "default",
            CreationTimestamp: metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
        },
        InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-1"},
        Reason:         "BackOff",
        Message:        "Back-off restarting failed container",
        Type:           "Warning",
        Count:          5,
    }

    fc := fake.NewSimpleClientset(&ev)
    result := getEventsImpl(fc, "prod", "default", 20)

    var r EventListResult
    if err := json.Unmarshal([]byte(result), &r); err != nil {
        t.Fatalf("invalid JSON: %v\n%s", err, result)
    }
    if r.Total != 1 {
        t.Errorf("expected 1 event, got %d", r.Total)
    }
    if r.Items[0].Type != "Warning" {
        t.Errorf("expected Warning event, got %s", r.Items[0].Type)
    }
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./tools/... -run TestGetEventsImpl -v
```

- [ ] **Step 3: 实现 tools/event.go**

```go
package tools

import (
    "context"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

// RegisterEventTools 注册事件相关 MCP 工具
func RegisterEventTools(s *server.MCPServer, mgr ClusterManagerInterface) {
    s.AddTool(mcp.NewTool("get_events",
        mcp.WithDescription("获取指定集群（和命名空间）的 K8s 事件"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("namespace", mcp.Description("命名空间，留空获取所有")),
        mcp.WithNumber("limit", mcp.Description("返回数量上限，默认 20")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        namespace, _ := req.Params.Arguments["namespace"].(string)
        limit := 20
        if l, ok := req.Params.Arguments["limit"].(float64); ok && l > 0 {
            limit = int(l)
        }
        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(getEventsImpl(client, cluster, namespace, limit)), nil
    })
}

func getEventsImpl(client kubernetes.Interface, cluster, namespace string, limit int) string {
    events, err := client.CoreV1().Events(namespace).List(context.Background(), metav1.ListOptions{
        Limit: int64(limit),
    })
    if err != nil {
        return toolError(fmt.Sprintf("list events 失败: %v", err))
    }

    items := make([]EventBrief, 0, len(events.Items))
    for _, e := range events.Items {
        items = append(items, EventBrief{
            Namespace: e.Namespace,
            Kind:      e.InvolvedObject.Kind,
            Name:      e.InvolvedObject.Name,
            Reason:    e.Reason,
            Message:   e.Message,
            Type:      e.Type,
            Count:     e.Count,
            Age:       ageString(e.CreationTimestamp.Time),
        })
    }

    return toJSON(EventListResult{
        Cluster:   cluster,
        Namespace: namespace,
        Total:     len(items),
        Items:     items,
    })
}
```

- [ ] **Step 4: 在 main.go 中注册**

添加：
```go
tools.RegisterEventTools(s, mgr)
```

- [ ] **Step 5: 运行测试**

```bash
go test ./tools/... -run TestGetEventsImpl -v
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add tools/event.go tools/event_test.go main.go
git commit -m "feat: add get_events tool"
```

---

## Task 12: tools/resource.go — describe_resource / apply_manifest / delete_resource

**Files:**
- Create: `tools/resource.go`

注意：`describe_resource` 和 `apply_manifest` 使用 dynamic client，需要额外依赖：
```bash
go get k8s.io/client-go/dynamic
go get k8s.io/client-go/restmapper
go get k8s.io/apimachinery/pkg/runtime/schema
```

- [ ] **Step 1: 添加 dynamic client 依赖**

```bash
go get k8s.io/client-go/dynamic
```

- [ ] **Step 2: 更新 k8s/manager.go 的接口暴露 RestConfig**

在 `ClusterManager` 中添加：

```go
// 在 ClusterManager struct 中添加 configs 字段
configs map[string]*rest.Config

// GetConfig 返回指定集群的 rest.Config
func (m *ClusterManager) GetConfig(clusterName string) (*rest.Config, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    cfg, ok := m.configs[clusterName]
    if !ok {
        return nil, fmt.Errorf("集群 %q 配置不存在", clusterName)
    }
    return cfg, nil
}
```

在 `NewClusterManager` 的 goroutine 中同时保存 config：
```go
type result struct {
    name   string
    client kubernetesClient
    config *rest.Config
    err    error
}
// goroutine 中:
ch <- result{name: n, client: client, config: cfg}

// 接收时:
m.clients[r.name] = r.client
m.configs[r.name] = r.config
```

- [ ] **Step 3: 实现 tools/resource.go**

```go
package tools

import (
    "context"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/apimachinery/pkg/util/yaml"
    "k8s.io/client-go/dynamic"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/restmapper"
    "k8s.io/rest"
)

// ResourceManagerInterface 扩展接口，支持获取 rest.Config
type ResourceManagerInterface interface {
    ClusterManagerInterface
    GetConfig(clusterName string) (*rest.Config, error)
}

// RegisterResourceTools 注册资源操作 MCP 工具
func RegisterResourceTools(s *server.MCPServer, mgr ResourceManagerInterface) {
    // describe_resource
    s.AddTool(mcp.NewTool("describe_resource",
        mcp.WithDescription("获取任意 K8s 资源的详细信息（通用）"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
        mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Deployment、Service、ConfigMap")),
        mcp.WithString("name", mcp.Required(), mcp.Description("资源名称")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        namespace := req.Params.Arguments["namespace"].(string)
        kind := req.Params.Arguments["kind"].(string)
        name := req.Params.Arguments["name"].(string)

        cfg, err := mgr.GetConfig(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(describeResourceImpl(ctx, client, cfg, cluster, namespace, kind, name)), nil
    })

    // apply_manifest
    s.AddTool(mcp.NewTool("apply_manifest",
        mcp.WithDescription("应用 YAML 清单到指定集群（server-side apply）"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("yaml_content", mcp.Required(), mcp.Description("YAML 格式的资源清单")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        yamlContent := req.Params.Arguments["yaml_content"].(string)

        cfg, err := mgr.GetConfig(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(applyManifestImpl(ctx, cfg, cluster, yamlContent)), nil
    })

    // delete_resource
    s.AddTool(mcp.NewTool("delete_resource",
        mcp.WithDescription("删除指定集群中的 K8s 资源"),
        mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
        mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
        mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Pod、Deployment")),
        mcp.WithString("name", mcp.Required(), mcp.Description("资源名称")),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        cluster := req.Params.Arguments["cluster"].(string)
        namespace := req.Params.Arguments["namespace"].(string)
        kind := req.Params.Arguments["kind"].(string)
        name := req.Params.Arguments["name"].(string)

        cfg, err := mgr.GetConfig(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        client, err := mgr.Get(cluster)
        if err != nil {
            return mcp.NewToolResultText(toolError(err.Error())), nil
        }
        return mcp.NewToolResultText(deleteResourceImpl(ctx, client, cfg, cluster, namespace, kind, name)), nil
    })
}

func describeResourceImpl(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, cluster, namespace, kind, name string) string {
    dynClient, gvr, err := buildDynamic(ctx, client, cfg, kind)
    if err != nil {
        return toolError(err.Error())
    }

    obj, err := dynClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return toolError(fmt.Sprintf("get resource 失败: %v", err))
    }

    // 移除噪音字段
    unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
    unstructured.RemoveNestedField(obj.Object, "metadata", "annotations")

    spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
    status, _, _ := unstructured.NestedMap(obj.Object, "status")
    labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")

    creationTime, _, _ := unstructured.NestedString(obj.Object, "metadata", "creationTimestamp")
    age := ""
    if t, err := time.Parse(time.RFC3339, creationTime); err == nil {
        age = ageString(t)
    }

    return toJSON(ResourceDetail{
        Cluster:   cluster,
        Kind:      kind,
        Namespace: namespace,
        Name:      name,
        Age:       age,
        Labels:    labels,
        Spec:      spec,
        Status:    status,
    })
}

func applyManifestImpl(ctx context.Context, cfg *rest.Config, cluster, yamlContent string) string {
    // 解析 YAML 为 unstructured
    obj := &unstructured.Unstructured{}
    dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(yamlContent), 4096)
    if err := dec.Decode(&obj.Object); err != nil {
        return toolError(fmt.Sprintf("解析 YAML 失败: %v", err))
    }

    dynClient, err := dynamic.NewForConfig(cfg)
    if err != nil {
        return toolError(fmt.Sprintf("创建 dynamic client 失败: %v", err))
    }

    // 通过 discovery 找到 GVR
    discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
    if err != nil {
        return toolError(fmt.Sprintf("创建 discovery client 失败: %v", err))
    }

    gvk := obj.GroupVersionKind()
    mapper, err := restmapper.GetAPIGroupResources(discoveryClient)
    if err != nil {
        return toolError(fmt.Sprintf("获取 API groups 失败: %v", err))
    }
    rm := restmapper.NewDiscoveryRESTMapper(mapper)
    mapping, err := rm.RESTMapping(gvk.GroupKind(), gvk.Version)
    if err != nil {
        return toolError(fmt.Sprintf("找不到 %s 的 REST mapping: %v", gvk.Kind, err))
    }

    namespace := obj.GetNamespace()
    var dr dynamic.ResourceInterface
    if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
        dr = dynClient.Resource(mapping.Resource).Namespace(namespace)
    } else {
        dr = dynClient.Resource(mapping.Resource)
    }

    // Server-side apply
    data, err := json.Marshal(obj)
    if err != nil {
        return toolError(fmt.Sprintf("序列化对象失败: %v", err))
    }
    result, err := dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
        FieldManager: "k8s-mcp",
        Force:        boolPtr(true),
    })
    if err != nil {
        return toolError(fmt.Sprintf("apply 失败: %v", err))
    }

    return toJSON(map[string]interface{}{
        "cluster":    cluster,
        "kind":       result.GetKind(),
        "namespace":  result.GetNamespace(),
        "name":       result.GetName(),
        "api_version": result.GetAPIVersion(),
        "status":     "applied",
    })
}

func deleteResourceImpl(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, cluster, namespace, kind, name string) string {
    dynClient, gvr, err := buildDynamic(ctx, client, cfg, kind)
    if err != nil {
        return toolError(err.Error())
    }

    if err := dynClient.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
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

// buildDynamic 创建 dynamic client 并解析 kind 对应的 GVR
func buildDynamic(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, kind string) (dynamic.Interface, schema.GroupVersionResource, error) {
    dynClient, err := dynamic.NewForConfig(cfg)
    if err != nil {
        return nil, schema.GroupVersionResource{}, fmt.Errorf("创建 dynamic client 失败: %w", err)
    }

    discoveryClient := client.Discovery()
    groups, err := restmapper.GetAPIGroupResources(discoveryClient)
    if err != nil {
        return nil, schema.GroupVersionResource{}, fmt.Errorf("获取 API groups 失败: %w", err)
    }

    rm := restmapper.NewDiscoveryRESTMapper(groups)
    // 先尝试无 Group，让 mapper 自己找
    mappings, err := rm.RESTMappings(schema.GroupKind{Kind: kind})
    if err != nil || len(mappings) == 0 {
        return nil, schema.GroupVersionResource{}, fmt.Errorf("找不到 Kind %q 的 REST mapping", kind)
    }

    return dynClient, mappings[0].Resource, nil
}

func boolPtr(b bool) *bool { return &b }
```

注意：`applyManifestImpl` 需要额外 import：
```go
"encoding/json"
"strings"
"time"

"k8s.io/apimachinery/pkg/api/meta"
"k8s.io/apimachinery/pkg/types"
"k8s.io/client-go/discovery"
```

- [ ] **Step 4: 更新 main.go 使用 ResourceManagerInterface**

将 `main.go` 中的：
```go
tools.RegisterClusterTools(s, mgr)
tools.RegisterPodTools(s, mgr)
tools.RegisterNamespaceTools(s, mgr)
tools.RegisterNodeTools(s, mgr)
tools.RegisterDeploymentTools(s, mgr)
tools.RegisterEventTools(s, mgr)
```
后面添加：
```go
tools.RegisterResourceTools(s, mgr)
```

同时 `mgr` 已经实现了 `GetConfig` 方法（Task 12 Step 2 中添加），所以满足 `ResourceManagerInterface`。

- [ ] **Step 5: 编译**

```bash
go mod tidy
go build ./...
```

修复所有编译错误。

- [ ] **Step 6: Commit**

```bash
git add tools/resource.go k8s/manager.go main.go go.mod go.sum
git commit -m "feat: add describe_resource, apply_manifest, delete_resource tools"
```

---

## Task 13: CLAUDE.md + Makefile + 全量测试

**Files:**
- Create: `CLAUDE.md`
- Create: `Makefile`

- [ ] **Step 1: 创建 CLAUDE.md**

```markdown
# k8s-mcp 开发说明

## 项目定位
多集群 Kubernetes MCP Server，供 Claude Code 调用管理 K8s 集群。
通过 stdio 接入，支持 list/describe/apply/delete 等操作。

## 关键约定
- 所有 Tool 必须带 `cluster` 参数，不允许有隐式"当前集群"概念
- 返回结构体定义在 `tools/types.go`，新增 Tool 遵循现有结构风格
- 列表类 Tool 必须支持 limit 参数，默认 20
- 日志类 Tool 必须支持 tail 参数，默认 100
- 所有 impl 函数（如 listPodsImpl）独立于 MCP 框架，便于单元测试

## 常用命令
- 编译：`make build`
- 测试：`make test`
- 整理依赖：`go mod tidy`

## MCP Tool 清单
- list_clusters, list_namespaces, list_nodes, describe_node
- list_pods, get_pod_logs, describe_pod
- list_deployments, scale_deployment
- get_events
- describe_resource, apply_manifest, delete_resource

## 目录结构
- config/: kubeconfig 扫描加载
- k8s/: 多集群 client 管理
- tools/: MCP 工具注册与实现
```

- [ ] **Step 2: 创建 Makefile**

```makefile
.PHONY: build test lint clean

BINARY=k8s-mcp
GO=go

build:
	$(GO) build -o $(BINARY) ./main.go

test:
	$(GO) test ./... -v -count=1

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)

.DEFAULT_GOAL := build
```

- [ ] **Step 3: 运行全量测试**

```bash
cd /Volumes/data/personal/code/k8s-mcp
make test
```

期望：所有测试 PASS，无编译错误

- [ ] **Step 4: 编译二进制**

```bash
make build
ls -la k8s-mcp
```

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md Makefile
git commit -m "chore: add CLAUDE.md dev guide and Makefile"
```

---

## Task 14: 配置 Claude Code MCP 接入

**Files:**
- Modify: `~/.claude/mcp.json` 或本地 `.claude/mcp.json`

- [ ] **Step 1: 编译并安装二进制**

```bash
make build
sudo cp k8s-mcp /usr/local/bin/k8s-mcp
```

或者直接用项目路径：
```bash
ls /Volumes/data/personal/code/k8s-mcp/k8s-mcp
```

- [ ] **Step 2: 确认 kubeconfig 目录存在**

```bash
ls ~/.kube/clusters/
```

如果目录不存在则创建，并将集群 kubeconfig 放入：
```bash
mkdir -p ~/.kube/clusters
# 将已有 kubeconfig 复制进去，文件名=集群名.yaml
```

- [ ] **Step 3: 配置 MCP Server**

编辑 `~/.claude/mcp.json`（或通过 `/mcp` 命令添加），添加：

```json
{
  "mcpServers": {
    "k8s": {
      "command": "/Volumes/data/personal/code/k8s-mcp/k8s-mcp",
      "args": ["--kubeconfig-dir", "~/.kube/clusters"]
    }
  }
}
```

- [ ] **Step 4: 重启 Claude Code 并验证**

重启后，运行：
```
list_clusters
```

期望看到已加载的集群列表。

- [ ] **Step 5: 最终提交**

```bash
git add .
git commit -m "chore: finalize k8s-mcp server implementation"
```

---

## 自检清单

- [x] Phase 1 覆盖：list_clusters, list_pods, MCP 通信 ✓ (Task 6, 7)
- [x] Phase 2 覆盖：list_namespaces, list_nodes, describe_node, get_pod_logs, describe_pod, list_deployments, get_events, describe_resource ✓ (Task 8-12)
- [x] Phase 3 覆盖：apply_manifest, delete_resource, scale_deployment ✓ (Task 10, 12)
- [x] 所有列表类 Tool 有 limit 参数 ✓
- [x] 日志 Tool 有 tail 参数 ✓
- [x] 返回 JSON 不返回原始 K8s 对象 ✓
- [x] 连接失败不影响其他集群 ✓ (Task 3)
- [x] 每个功能有独立 impl 函数供测试 ✓
- [x] 每个 Task 完成后 commit ✓
