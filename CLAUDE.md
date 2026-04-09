# k8s-mcp 开发说明

## 项目定位

多集群 Kubernetes MCP Server，供 Claude Code 调用管理 K8s 集群。
通过 stdio 接入，支持列表查询、详情描述、资源操作等功能。

## 关键约定

- 所有 Tool 必须带 `cluster` 参数，不允许有隐式"当前集群"概念
- 返回结构体定义在 `tools/types.go`，新增 Tool 遵循现有结构风格
- 列表类 Tool 必须支持 `limit` 参数，默认 20
- 日志类 Tool 必须支持 `tail` 参数，默认 100
- 所有 impl 函数（如 `listPodsImpl`）独立于 MCP 框架，便于单元测试
- 必填参数使用带 ok 检查的类型断言，防止 panic
- ctx 必须从 handler 传入 impl 函数，不使用 context.Background()

## MCP Tool 清单

| Tool | 参数 | 说明 |
|------|------|------|
| `list_clusters` | 无 | 列出所有集群及连接状态 |
| `list_namespaces` | cluster | 列出命名空间 |
| `list_nodes` | cluster | 列出节点 |
| `describe_node` | cluster, name | 节点详情 |
| `list_pods` | cluster, namespace, label_selector?, limit? | 列出 Pod |
| `get_pod_logs` | cluster, namespace, pod, container?, tail? | 获取日志 |
| `describe_pod` | cluster, namespace, name | Pod 详情 |
| `list_deployments` | cluster, namespace | 列出 Deployment |
| `scale_deployment` | cluster, namespace, name, replicas | 调整副本数 |
| `get_events` | cluster, namespace?, limit? | 获取事件 |
| `describe_resource` | cluster, namespace, kind, name | 通用资源描述 |
| `apply_manifest` | cluster, yaml_content | 应用 YAML |
| `delete_resource` | cluster, namespace, kind, name | 删除资源 |

## 目录结构

```
k8s-mcp/
├── main.go              # 入口，注册所有工具，启动 stdio MCP Server
├── config/
│   └── loader.go        # 扫描 kubeconfig 目录
├── k8s/
│   └── manager.go       # 多集群连接池
└── tools/
    ├── types.go         # 所有返回结构体定义
    ├── helpers.go       # ageString / toJSON / toolError
    ├── cluster.go       # list_clusters + ClusterManagerInterface
    ├── namespace.go     # list_namespaces
    ├── node.go          # list_nodes / describe_node
    ├── pod.go           # list_pods / get_pod_logs / describe_pod
    ├── deployment.go    # list_deployments / scale_deployment
    ├── event.go         # get_events
    └── resource.go      # describe_resource / apply_manifest / delete_resource
```

## 常用命令

```bash
make build    # 编译
make test     # 运行全量测试
go mod tidy   # 整理依赖
```

## kubeconfig 约定

将各集群 kubeconfig 存放在同一目录，文件名即为集群名：

```
~/.kube/clusters/
  prod.yaml    → 集群名: prod
  dev.yaml     → 集群名: dev
```

启动参数：`--kubeconfig-dir ~/.kube/clusters`
