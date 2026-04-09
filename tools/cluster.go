package tools

import (
	"context"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"k8s.io/client-go/kubernetes"
)

// ClusterManagerInterface 供 tools 包调用的集群管理接口
// 后续其他 Tool 文件都依赖此接口
type ClusterManagerInterface interface {
	Get(clusterName string) (kubernetes.Interface, error)
	ListNames() []string
	ListErrors() map[string]string
}

// clusterLister 仅用于 buildClusterList 的最小接口，方便测试
type clusterLister interface {
	ListNames() []string
	ListErrors() map[string]string
}

// RegisterClusterTools 注册集群相关 MCP 工具
func RegisterClusterTools(s *server.MCPServer, mgr ClusterManagerInterface) {
	s.AddTool(
		mcp.NewTool("list_clusters",
			mcp.WithDescription("列出所有已加载的 K8s 集群及其连接状态"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText(buildClusterList(mgr)), nil
		},
	)
}

// buildClusterList 构建集群列表 JSON，ok 集群在前，error 集群在后，各自按名称排序
func buildClusterList(mgr clusterLister) string {
	names := mgr.ListNames()
	errs := mgr.ListErrors()

	sort.Strings(names)

	items := make([]ClusterInfo, 0, len(names)+len(errs))

	// 正常集群
	for _, name := range names {
		items = append(items, ClusterInfo{Name: name, Status: "ok"})
	}

	// 异常集群（按名称排序）
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
