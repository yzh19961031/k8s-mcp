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

func listResourceQuotasImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace string) string {
	quotas, err := client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return toolError(fmt.Sprintf("list resource quotas 失败: %v", err))
	}

	items := make([]ResourceQuotaBrief, 0, len(quotas.Items))
	for _, q := range quotas.Items {
		hard := make(map[string]string, len(q.Spec.Hard))
		for k, v := range q.Spec.Hard {
			hard[string(k)] = v.String()
		}
		used := make(map[string]string, len(q.Status.Used))
		for k, v := range q.Status.Used {
			used[string(k)] = v.String()
		}
		items = append(items, ResourceQuotaBrief{
			Name:      q.Name,
			Namespace: q.Namespace,
			Age:       ageString(q.CreationTimestamp.Time),
			Hard:      hard,
			Used:      used,
		})
	}

	return toJSON(ResourceQuotaListResult{
		Cluster:   cluster,
		Namespace: namespace,
		Total:     len(items),
		Items:     items,
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
