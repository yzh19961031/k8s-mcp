# k8s-mcp

一个支持**多集群**的 Kubernetes MCP Server，供 [Claude Code](https://claude.ai/code) 通过 stdio 调用，实现对多个 K8s 集群的统一纳管和操作。

> 解决了 `mcp-server-kubernetes` 不支持多集群的痛点。

---

## 功能特性

- **多集群支持**：通过目录扫描自动加载多个 kubeconfig，并发初始化连接
- **结构化返回**：所有工具返回 JSON 摘要而非原始 K8s 对象，减少 context 污染
- **13 个 MCP 工具**：覆盖集群查询、Pod 操作、Deployment 管理、事件查看、通用资源操作
- **只读/写操作分离**：查询类和写操作类工具分开，安全可控
- **热重载**：支持新增集群 kubeconfig 后不重启服务（`Reload` 方法）

---

## 工具清单

### 集群管理

| 工具 | 参数 | 说明 |
|------|------|------|
| `list_clusters` | 无 | 列出所有已加载集群及连接状态 |

### 资源查询（只读）

| 工具 | 参数 | 说明 |
|------|------|------|
| `list_namespaces` | cluster | 列出命名空间 |
| `list_nodes` | cluster | 列出节点及状态 |
| `describe_node` | cluster, name | 节点详情（容量/条件/地址） |
| `list_pods` | cluster, namespace, label_selector?, limit? | 列出 Pod 摘要 |
| `get_pod_logs` | cluster, namespace, pod, container?, tail? | 获取容器日志 |
| `describe_pod` | cluster, namespace, name | Pod 详情（容器状态/条件） |
| `list_deployments` | cluster, namespace | 列出 Deployment |
| `describe_resource` | cluster, namespace, kind, name | 通用资源描述（支持任意 Kind） |
| `get_events` | cluster, namespace?, limit? | 获取 K8s 事件 |

### 资源操作（写操作）

| 工具 | 参数 | 说明 |
|------|------|------|
| `apply_manifest` | cluster, yaml_content | Server-side apply YAML 清单 |
| `delete_resource` | cluster, namespace, kind, name | 删除资源 |
| `scale_deployment` | cluster, namespace, name, replicas | 调整副本数 |

---

## 快速开始

### 1. 准备 kubeconfig

将各集群的 kubeconfig 文件统一放到一个目录，**文件名即为集群名**：

```bash
mkdir -p ~/.kube/clusters

# 文件名 = 集群名
cp /path/to/prod-kubeconfig     ~/.kube/clusters/prod.yaml
cp /path/to/dev-kubeconfig      ~/.kube/clusters/dev.yaml
cp /path/to/staging-kubeconfig  ~/.kube/clusters/staging.yaml
```

### 2. 编译

```bash
git clone https://github.com/yzh19961031/k8s-mcp.git
cd k8s-mcp
make build
```

### 3. 接入 Claude Code

在项目根目录（或 `~/.claude/`）新建 `.mcp.json`：

```json
{
  "mcpServers": {
    "k8s": {
      "command": "/path/to/k8s-mcp",
      "args": ["--kubeconfig-dir", "~/.kube/clusters"]
    }
  }
}
```

重启 Claude Code，信任该 MCP Server 后即可使用。

---

## 使用示例

在 Claude Code 对话中直接描述意图，AI 会自动调用对应工具：

```
# 查看所有集群
list_clusters

# 查看 prod 集群 default 命名空间下的 Pod
list_pods cluster=prod namespace=default

# 获取某个 Pod 的最近 50 行日志
get_pod_logs cluster=prod namespace=default pod=web-abc123 tail=50

# 查看 Warning 事件
get_events cluster=prod namespace=default

# 应用一个 YAML
apply_manifest cluster=dev yaml_content=<yaml内容>

# 扩容 Deployment
scale_deployment cluster=prod namespace=default name=web replicas=5
```

---

## 技术选型

| 项目 | 选型 |
|------|------|
| 语言 | Go 1.22+ |
| K8s 客户端 | `k8s.io/client-go` |
| MCP 框架 | `github.com/mark3labs/mcp-go` |
| 通信方式 | stdio（Claude Code 标准接入方式） |
| 配置方式 | 目录扫描多个 kubeconfig 文件 |

---

## 目录结构

```
k8s-mcp/
├── main.go              # 入口：解析参数、注册工具、启动 MCP Server
├── config/
│   └── loader.go        # kubeconfig 目录扫描
├── k8s/
│   └── manager.go       # 多集群连接池（并发初始化、Get/Reload）
└── tools/
    ├── types.go         # 所有返回结构体定义
    ├── helpers.go       # 公共辅助函数（ageString/toJSON/toolError）
    ├── cluster.go       # list_clusters
    ├── namespace.go     # list_namespaces
    ├── node.go          # list_nodes / describe_node
    ├── pod.go           # list_pods / get_pod_logs / describe_pod
    ├── deployment.go    # list_deployments / scale_deployment
    ├── event.go         # get_events
    └── resource.go      # describe_resource / apply_manifest / delete_resource
```

---

## 开发

```bash
# 运行测试
make test

# 编译
make build

# 整理依赖
go mod tidy
```

### 设计约定

- 所有工具必须带 `cluster` 参数，无隐式"当前集群"概念
- 列表类工具支持 `limit` 参数，默认 20
- 日志工具支持 `tail` 参数，默认 100
- 返回 JSON 而非 YAML，不含 `managedFields` 等噪音字段
- `impl` 函数（如 `listPodsImpl`）独立于 MCP 框架，便于单元测试

---

## License

MIT
