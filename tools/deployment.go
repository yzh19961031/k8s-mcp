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
		args := req.GetArguments()
		client, cluster, err := getClusterClient(args, mgr)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, err := mustString(args, "namespace")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listDeploymentsImpl(ctx, client, cluster, namespace)), nil
	})

	s.AddTool(mcp.NewTool("scale_deployment",
		mcp.WithDescription("调整指定 Deployment 的副本数"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Deployment 名称")),
		mcp.WithNumber("replicas", mcp.Required(), mcp.Description("目标副本数")),
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
		name, err := mustString(args, "name")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		replicasF, ok := args["replicas"].(float64)
		if !ok {
			return mcp.NewToolResultText(toolError("参数 replicas 无效")), nil
		}
		return mcp.NewToolResultText(scaleDeploymentImpl(ctx, client, cluster, namespace, name, int32(replicasF))), nil
	})
}

func listDeploymentsImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace string) string {
	deps, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
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
