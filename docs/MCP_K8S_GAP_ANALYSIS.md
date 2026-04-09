# K8s MCP 工具缺口分析

> 背景：在排查 h800 集群 `yw-test-h800` 命名空间下 `test-yuan-h800` Deployment 无法拉起 GPU Pod 的问题时，发现现有 MCP 工具存在以下能力缺失。

---

## 一、现有 MCP 服务说明

| MCP Server | 说明 | 覆盖集群 |
|---|---|---|
| `mcp__k8s__*` | 自研 K8s MCP，支持多集群 | 4090 / 48 / h800 |
| `mcp__kubernetes-mcp-server__*` | 开源 kubernetes-mcp-server | 仅连接单一集群（非 h800） |

---

## 二、工具缺口详细清单

### 2.1 `describe_resource` 不支持核心资源类型

**现象：**

```
describe_resource(kind=Deployment) → "找不到 Kind \"Deployment\" 的 REST mapping"
describe_resource(kind=ReplicaSet) → "找不到 Kind \"ReplicaSet\" 的 REST mapping"
```

**影响：**
- 无法直接查看 Deployment 的 Pod 模板（资源请求、PVC 挂载、环境变量、节点选择器等）
- 无法查看 ReplicaSet 的状态和 Conditions（RS 是 Pod 为什么没被创建的关键信息源）

**建议：**
- `describe_resource` 补充对 `apps/v1` 下资源的 REST mapping 支持（Deployment、ReplicaSet、StatefulSet、DaemonSet）

---

### 2.2 缺少 ReplicaSet 专用列表/查询工具

**现象：**
- `list_deployments` 存在，但没有对应的 `list_replicasets`
- Deployment 事件显示 RS 已 scale up，但 Pod 没出现 → 需要查 RS 的 Conditions 和 Events

**影响：**
- 排查"Deployment 已触发但 Pod 未创建"这类问题时，必须借助 RS 的状态，现在完全看不到

**建议：**
- 新增 `list_replicasets(cluster, namespace)` 工具
- 或扩展 `describe_resource` 支持 ReplicaSet

---

### 2.3 `get_pod_logs` 在 kube-system 命名空间失败

**现象：**

```
get_pod_logs(cluster=h800, namespace=kube-system, pod=hami-scheduler-...) 
→ "获取日志失败: the server rejected our request for an unknown reason"
```

**影响：**
- GPU 调度问题的核心日志在 `hami-scheduler` 和 `hami-device-plugin` 里
- 无法获取 kube-system 日志，等于无法排查 GPU 调度失败原因

**建议：**
- 检查 MCP Server 使用的 ServiceAccount 是否有 `kube-system` 命名空间的 `pods/log` 权限，必要时补充 ClusterRole

---

### 2.4 缺少 ResourceQuota 列表工具

**现象：**
- 只能通过 `describe_resource(kind=ResourceQuota, name=xxx)` 按名称查，无法列举命名空间内所有 Quota

**影响：**
- 不知道 Quota 叫什么名字就没法查，需要猜测名称逐个尝试

**建议：**
- 新增 `list_resource_quotas(cluster, namespace)` 工具
- 或扩展 `describe_resource` 支持按 namespace 列举所有同类资源

---

### 2.5 `kubernetes-mcp-server` 仅支持单集群

**现象：**
- `resources_get(kind=Deployment, name=test-yuan-h800, namespace=yw-test-h800)` → "not found"
- `resources_list(kind=ReplicaSet, namespace=yw-test-h800)` → 无输出

该工具连接的是其他集群，不是 h800。

**影响：**
- kubernetes-mcp-server 的更丰富的 API（resources_get/list/create_or_update）只对默认集群可用，多集群场景下无法使用

**建议：**
- 统一用自研 `mcp__k8s__*`（已支持多集群），或者让 `kubernetes-mcp-server` 也支持传入 `cluster` 参数

---

### 2.6 缺少 Pod Exec 能力

**现象：** 无任何 MCP 工具支持在 Pod 内执行命令

**影响：**
- 无法在容器内排查网络、文件挂载、进程等问题
- `mcp__kubernetes-mcp-server__pods_exec` 工具存在但只对默认集群生效

**建议：**
- 自研 MCP 新增 `exec_pod(cluster, namespace, pod, container, command)` 工具

---

### 2.7 事件查询缺少资源关联过滤

**现象：**
- `get_events(cluster, namespace)` 只能按命名空间查，无法过滤特定资源（如只看某个 Deployment 或 Pod 的事件）

**影响：**
- 命名空间事件多时，目标资源的事件被淹没

**建议：**
- 新增 `field_selector` 或 `involved_object` 参数支持过滤，例如：
  ```
  get_events(cluster=h800, namespace=yw-test-h800, involved_object_kind=ReplicaSet, involved_object_name=test-yuan-h800-596fb6f59d)
  ```

---

## 三、优先级汇总

| 优先级 | 缺口 | 排查阻塞程度 |
|---|---|---|
| P0 | kube-system 日志权限 | 完全无法排查 GPU 调度问题 |
| P0 | describe_resource 支持 Deployment/RS | 看不到 Pod 模板和 RS 状态 |
| P1 | list_replicasets | 定位"Pod 未创建"根因 |
| P1 | list_resource_quotas | 快速排查配额限制 |
| P2 | exec_pod 多集群支持 | 容器内深度排查 |
| P2 | get_events 按资源过滤 | 减少噪音，聚焦目标 |
| P3 | kubernetes-mcp-server 多集群支持 | 当前可用自研 MCP 替代 |
