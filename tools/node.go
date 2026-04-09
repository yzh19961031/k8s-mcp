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
		args := req.GetArguments()
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		client, err := mgr.Get(cluster)
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
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		name, ok2 := args["name"].(string)
		if !ok2 || name == "" {
			return mcp.NewToolResultText(toolError("参数 name 无效")), nil
		}
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(describeNodeImpl(ctx, client, cluster, name)), nil
	})
}

func listNodesImpl(ctx context.Context, client kubernetes.Interface, cluster string) string {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
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

func describeNodeImpl(ctx context.Context, client kubernetes.Interface, cluster, name string) string {
	n, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
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
	var roles []string
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
