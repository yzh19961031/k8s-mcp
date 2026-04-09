package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RegisterEventTools 注册事件相关 MCP 工具
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
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		namespace, _ := args["namespace"].(string)
		limit := 20
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		involvedKind, _ := args["involved_object_kind"].(string)
		involvedName, _ := args["involved_object_name"].(string)
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(getEventsImpl(ctx, client, cluster, namespace, involvedKind, involvedName, limit)), nil
	})
}

func getEventsImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace, involvedKind, involvedName string, limit int) string {
	var selectors []string
	if involvedKind != "" {
		selectors = append(selectors, "involvedObject.kind="+involvedKind)
	}
	if involvedName != "" {
		selectors = append(selectors, "involvedObject.name="+involvedName)
	}

	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		Limit:         int64(limit),
		FieldSelector: strings.Join(selectors, ","),
	})
	if err != nil {
		return toolError(fmt.Sprintf("list events 失败: %v", err))
	}

	items := make([]EventBrief, 0, len(events.Items))
	for _, e := range events.Items {
		item := EventBrief{
			Namespace: e.Namespace,
			Kind:      e.InvolvedObject.Kind,
			Name:      e.InvolvedObject.Name,
			Reason:    e.Reason,
			Message:   e.Message,
			Type:      e.Type,
			Count:     e.Count,
			Age:       ageString(e.CreationTimestamp.Time),
		}
		if !e.LastTimestamp.IsZero() {
			item.LastTimestamp = ageString(e.LastTimestamp.Time)
		}
		items = append(items, item)
	}

	return toJSON(EventListResult{
		Cluster:   cluster,
		Namespace: namespace,
		Total:     len(items),
		Items:     items,
	})
}
