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
		args := req.GetArguments()
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listNamespacesImpl(ctx, client, cluster)), nil
	})
}

func listNamespacesImpl(ctx context.Context, client kubernetes.Interface, cluster string) string {
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
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
