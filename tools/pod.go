package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RegisterPodTools 注册 Pod 相关 MCP 工具
func RegisterPodTools(s *server.MCPServer, mgr ClusterManagerInterface) {
	// list_pods
	s.AddTool(mcp.NewTool("list_pods",
		mcp.WithDescription("列出指定集群和命名空间下的 Pod 摘要"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间，空字符串表示所有命名空间")),
		mcp.WithString("label_selector", mcp.Description("标签选择器，例如 app=nginx")),
		mcp.WithNumber("limit", mcp.Description("返回数量上限，默认 20")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		namespace, ok := args["namespace"].(string)
		if !ok {
			return mcp.NewToolResultText(toolError("参数 namespace 无效")), nil
		}
		labelSelector, _ := args["label_selector"].(string)
		limit := 20
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listPodsImpl(ctx, client, cluster, namespace, labelSelector, limit)), nil
	})

	// get_pod_logs
	s.AddTool(mcp.NewTool("get_pod_logs",
		mcp.WithDescription("获取指定 Pod 的容器日志"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
		mcp.WithString("pod", mcp.Required(), mcp.Description("Pod 名称")),
		mcp.WithString("container", mcp.Description("容器名称，多容器时必填")),
		mcp.WithNumber("tail", mcp.Description("返回最后 N 行，默认 100")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		namespace, ok2 := args["namespace"].(string)
		if !ok2 {
			return mcp.NewToolResultText(toolError("参数 namespace 无效")), nil
		}
		pod, ok3 := args["pod"].(string)
		if !ok3 || pod == "" {
			return mcp.NewToolResultText(toolError("参数 pod 无效")), nil
		}
		container, _ := args["container"].(string)
		tail := int64(100)
		if t, ok := args["tail"].(float64); ok && t > 0 {
			tail = int64(t)
		}
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(getPodLogsImpl(ctx, client, cluster, namespace, pod, container, tail)), nil
	})

	// describe_pod
	s.AddTool(mcp.NewTool("describe_pod",
		mcp.WithDescription("获取指定 Pod 的详细信息"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Pod 名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		namespace, ok2 := args["namespace"].(string)
		if !ok2 {
			return mcp.NewToolResultText(toolError("参数 namespace 无效")), nil
		}
		name, ok3 := args["name"].(string)
		if !ok3 || name == "" {
			return mcp.NewToolResultText(toolError("参数 name 无效")), nil
		}
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(describePodImpl(ctx, client, cluster, namespace, name)), nil
	})
}

func listPodsImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace, labelSelector string, limit int) string {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		Limit:         int64(limit),
	})
	if err != nil {
		return toolError(fmt.Sprintf("list pods 失败: %v", err))
	}

	items := make([]PodBrief, 0, len(pods.Items))
	for _, p := range pods.Items {
		ready, total := countReady(p)
		restarts := countRestarts(p)
		items = append(items, PodBrief{
			Name:      p.Name,
			Namespace: p.Namespace,
			Status:    string(p.Status.Phase),
			Ready:     fmt.Sprintf("%d/%d", ready, total),
			Restarts:  restarts,
			Age:       ageString(p.CreationTimestamp.Time),
			NodeName:  p.Spec.NodeName,
		})
	}

	return toJSON(PodListResult{
		Cluster:   cluster,
		Namespace: namespace,
		Total:     len(items),
		Items:     items,
	})
}

func getPodLogsImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace, pod, container string, tail int64) string {
	opts := &corev1.PodLogOptions{TailLines: &tail}
	if container != "" {
		opts.Container = container
	}
	req := client.CoreV1().Pods(namespace).GetLogs(pod, opts)
	logs, err := req.DoRaw(ctx)
	if err != nil {
		return toolError(fmt.Sprintf("获取日志失败: %v", err))
	}
	return toJSON(map[string]interface{}{
		"cluster":   cluster,
		"namespace": namespace,
		"pod":       pod,
		"container": container,
		"tail":      tail,
		"logs":      string(logs),
	})
}

func describePodImpl(ctx context.Context, client kubernetes.Interface, cluster, namespace, name string) string {
	p, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return toolError(fmt.Sprintf("get pod 失败: %v", err))
	}

	containers := make([]ContainerInfo, 0, len(p.Status.ContainerStatuses))
	for _, cs := range p.Status.ContainerStatuses {
		containers = append(containers, ContainerInfo{
			Name:     cs.Name,
			Image:    cs.Image,
			Ready:    cs.Ready,
			Restarts: cs.RestartCount,
			State:    containerState(cs),
		})
	}

	conditions := make([]string, 0, len(p.Status.Conditions))
	for _, c := range p.Status.Conditions {
		conditions = append(conditions, fmt.Sprintf("%s=%s", c.Type, c.Status))
	}

	return toJSON(PodDetail{
		Cluster:    cluster,
		Namespace:  namespace,
		Name:       p.Name,
		Status:     string(p.Status.Phase),
		NodeName:   p.Spec.NodeName,
		Age:        ageString(p.CreationTimestamp.Time),
		Labels:     p.Labels,
		Containers: containers,
		Conditions: conditions,
	})
}

func countReady(p corev1.Pod) (ready, total int32) {
	for _, cs := range p.Status.ContainerStatuses {
		total++
		if cs.Ready {
			ready++
		}
	}
	return
}

func countRestarts(p corev1.Pod) int32 {
	var total int32
	for _, cs := range p.Status.ContainerStatuses {
		total += cs.RestartCount
	}
	return total
}

func containerState(cs corev1.ContainerStatus) string {
	if cs.State.Running != nil {
		return "Running"
	}
	if cs.State.Waiting != nil {
		return "Waiting: " + cs.State.Waiting.Reason
	}
	if cs.State.Terminated != nil {
		return "Terminated: " + cs.State.Terminated.Reason
	}
	return "Unknown"
}
