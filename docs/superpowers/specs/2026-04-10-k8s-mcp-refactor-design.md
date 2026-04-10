# k8s-mcp 重构设计文档

**日期**: 2026-04-10  
**范围**: 代码质量、性能优化、工具精简（第一轮，不含 GPU 专项功能）  
**第二轮**: GPU 专项功能（list_gpu_nodes / get_node_gpu_info / get_hami_vgpu_status）

---

## 背景与目标

当前 k8s-mcp 存在以下问题：

1. **性能**：`describe_resource` / `apply_manifest` / `delete_resource` 每次调用都重新做 K8s API Discovery（多次 HTTP 请求），高频调用下延迟明显
2. **工具数量**：每种资源类型一个 list tool，随着资源种类增加，tool schema 占用上下文 token 增多，影响模型选择准确率
3. **Pod 状态不准**：直接使用 `Phase` 字段，CrashLoopBackOff / OOMKilled 的 Pod 显示成 "Running"
4. **Handler 样板重复**：每个 tool handler 都有相同的参数提取 + 集群获取代码
5. **annotations 丢失**：`describe_resource` 删掉了所有 annotations，导致 HAMI 分配信息等关键数据丢失

---

## 变更清单

### 1. 新增 `k8s/dynamic_cache.go` — DynamicClientCache

**职责**: 按集群缓存 dynamic client 和 RESTMapper，首次访问时懒初始化，后续调用纯内存查找。

**接口设计**:

```go
type DynamicEntry struct {
    Client dynamic.Interface
    Mapper meta.RESTMapper
}

type DynamicClientCache struct {
    cache sync.Map // key: clusterName, value: *DynamicEntry
    mgr   ResourceManagerInterface
}

func NewDynamicClientCache(mgr ResourceManagerInterface) *DynamicClientCache
func (c *DynamicClientCache) Get(clusterName string) (*DynamicEntry, error)
```

**行为**:
- `Get` 先查 `sync.Map`，命中直接返回
- 未命中：调用 `mgr.GetConfig(clusterName)` 创建 dynamic client 和 RESTMapper，存入缓存后返回
- RESTMapper 使用 `restmapper.NewDiscoveryRESTMapper`，仅在首次初始化时调用 Discovery API
- 线程安全：使用 `sync.Map` 的 `LoadOrStore` 避免并发重复初始化

**影响**:
- `resource.go` 中的 `buildDynamic` 函数删除，改为调用 `DynamicClientCache.Get`
- `RegisterResourceTools` 接收 `*DynamicClientCache` 作为额外参数

---

### 2. 工具结构调整（`tools/`）

#### 保留的专属工具

| 工具 | 文件 | 保留原因 |
|------|------|---------|
| `list_pods` | `pod.go` | 最高频，有 ready/restarts/node_name 专属字段 |
| `list_deployments` | `deployment.go` | 次高频，有 ready/replicas 专属字段 |
| `list_nodes` | `node.go` | 节点场景固定，有 roles/version 专属字段 |

#### 删除的工具

| 工具 | 原因 |
|------|------|
| `list_replicasets` | 低频，用 `list_resources kind=ReplicaSet` 替代 |

#### 新增通用工具

**`list_resources`**（新增到 `resource.go`）:

```
参数:
  cluster        必填，集群名称
  kind           必填，资源类型，如 Service / Ingress / PVC / ConfigMap / StatefulSet 等
  namespace      可选，留空查所有命名空间
  label_selector 可选，标签过滤
  limit          可选，默认 20

返回:
  cluster / kind / namespace / total / items[]
  items 字段: name / namespace / age / labels（通用摘要，不含类型专属字段）
```

实现使用 `DynamicClientCache`，通过 dynamic client 列举任意资源类型。

---

### 3. Pod 状态判断修复（`tools/pod.go`）

删除 `Status: string(p.Status.Phase)` 的简单赋值，改为 `podDisplayStatus(p)` 函数。

**判断顺序**（模仿 kubectl）:

```
1. p.DeletionTimestamp != nil → "Terminating"

2. 遍历 initContainerStatuses:
   - Waiting.Reason == "PodInitializing" → 跳过
   - Waiting 状态 → "Init:" + Reason
   - Terminated.ExitCode != 0 → "Init:Error"
   - !Ready && RestartCount > 0 → "Init:CrashLoopBackOff"
   - 未完成的 init container → "Init:N/M"

3. 遍历 containerStatuses:
   - Waiting != nil → Waiting.Reason（如 "CrashLoopBackOff", "ImagePullBackOff"）
   - Terminated != nil && ExitCode != 0 → "OOMKilled" 或 "Error"

4. 兜底 → string(p.Status.Phase)
```

---

### 4. Handler 样板抽取（`tools/helpers.go`）

新增两个 helper 函数：

```go
// mustString 从 args 中提取字符串，为空时返回错误
func mustString(args map[string]any, key string) (string, error)

// getClusterClient 提取 cluster 参数并获取对应 client
func getClusterClient(args map[string]any, mgr ClusterManagerInterface) (kubernetes.Interface, string, error)
```

各 tool handler 统一改用这两个 helper，消除重复的类型断言代码。

---

### 5. `describe_resource` annotations 处理（`tools/resource.go`）

**当前行为**: 删除全部 annotations  
**改后行为**: 保留 annotations，过滤以下噪音字段：

```go
var noisyAnnotations = []string{
    "kubectl.kubernetes.io/last-applied-configuration", // 内容极长，是完整 JSON
    "deployment.kubernetes.io/revision",                 // 版本号，对 AI 无意义
}
```

其余 annotations 全部返回（包括 `hami.io/*` 等业务 annotations）。

---

### 6. `get_pod_logs` 补充 `previous` 参数（`tools/pod.go`）

新增可选参数 `previous: bool`，对应 `PodLogOptions.Previous`，用于查看已崩溃容器的历史日志。

---

## 文件变更汇总

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `k8s/dynamic_cache.go` | **新增** | DynamicClientCache 实现 |
| `tools/helpers.go` | **修改** | 新增 mustString / getClusterClient |
| `tools/resource.go` | **修改** | 删 buildDynamic，用 cache；新增 list_resources；修 annotations |
| `tools/pod.go` | **修改** | 修 Pod 状态判断；加 previous 参数 |
| `tools/deployment.go` | **修改** | 删 list_replicasets；handler 改用 helper |
| `tools/node.go` | **修改** | handler 改用 helper |
| `tools/namespace.go` | **修改** | handler 改用 helper |
| `tools/event.go` | **修改** | handler 改用 helper |
| `tools/cluster.go` | **修改** | handler 改用 helper |
| `tools/types.go` | **修改** | 删 ReplicaSetBrief/ReplicaSetListResult；新增 ResourceListResult |
| `main.go` | **修改** | 初始化 DynamicClientCache，传入 RegisterResourceTools |
| `README.md` | **修改** | 更新工具列表，删 list_replicasets，加 list_resources |

---

## 不在本轮范围内

- GPU 专项：`list_gpu_nodes` / `get_node_gpu_info` / `get_hami_vgpu_status`（第二轮）
- 节点运维：cordon / drain / label_node / taint_node（待规划）
- 监控指标：get_node_metrics / get_pod_metrics（需 metrics-server 依赖）
- 集群动态注册/注销

---

## 测试要点

- `DynamicClientCache.Get` 并发调用不重复初始化
- `list_resources` 支持 Service / Ingress / PVC / ConfigMap / StatefulSet 等常见 kind
- Pod 状态：CrashLoopBackOff / Terminating / Init 阶段 / OOMKilled 显示正确
- `describe_resource` 返回结果包含 `hami.io/*` annotations，不含 `last-applied-configuration`
- `get_pod_logs` `previous=true` 能拿到已终止容器的历史日志
