package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// ResourceManagerInterface 扩展 ClusterManagerInterface，支持获取 rest.Config
type ResourceManagerInterface interface {
	ClusterManagerInterface
	GetConfig(clusterName string) (*rest.Config, error)
}

// RegisterResourceTools 注册资源操作 MCP 工具
func RegisterResourceTools(s *server.MCPServer, mgr ResourceManagerInterface) {
	// describe_resource
	s.AddTool(mcp.NewTool("describe_resource",
		mcp.WithDescription("获取任意 K8s 资源的详细信息（通用）"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间，集群级资源传空字符串")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Deployment、Service、ConfigMap")),
		mcp.WithString("name", mcp.Required(), mcp.Description("资源名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		namespace, _ := args["namespace"].(string)
		kind, ok2 := args["kind"].(string)
		if !ok2 || kind == "" {
			return mcp.NewToolResultText(toolError("参数 kind 无效")), nil
		}
		name, ok3 := args["name"].(string)
		if !ok3 || name == "" {
			return mcp.NewToolResultText(toolError("参数 name 无效")), nil
		}

		cfg, err := mgr.GetConfig(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(describeResourceImpl(ctx, client, cfg, cluster, namespace, kind, name)), nil
	})

	// apply_manifest
	s.AddTool(mcp.NewTool("apply_manifest",
		mcp.WithDescription("应用 YAML 清单到指定集群（server-side apply）"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("yaml_content", mcp.Required(), mcp.Description("YAML 或 JSON 格式的资源清单")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		yamlContent, ok2 := args["yaml_content"].(string)
		if !ok2 || yamlContent == "" {
			return mcp.NewToolResultText(toolError("参数 yaml_content 无效")), nil
		}

		cfg, err := mgr.GetConfig(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(applyManifestImpl(ctx, client, cfg, cluster, yamlContent)), nil
	})

	// delete_resource
	s.AddTool(mcp.NewTool("delete_resource",
		mcp.WithDescription("删除指定集群中的 K8s 资源"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间，集群级资源传空字符串")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Pod、Deployment")),
		mcp.WithString("name", mcp.Required(), mcp.Description("资源名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, ok := args["cluster"].(string)
		if !ok || cluster == "" {
			return mcp.NewToolResultText(toolError("参数 cluster 无效")), nil
		}
		namespace, _ := args["namespace"].(string)
		kind, ok2 := args["kind"].(string)
		if !ok2 || kind == "" {
			return mcp.NewToolResultText(toolError("参数 kind 无效")), nil
		}
		name, ok3 := args["name"].(string)
		if !ok3 || name == "" {
			return mcp.NewToolResultText(toolError("参数 name 无效")), nil
		}

		cfg, err := mgr.GetConfig(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		client, err := mgr.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(deleteResourceImpl(ctx, client, cfg, cluster, namespace, kind, name)), nil
	})
}

// buildDynamic 创建 dynamic client 并解析 kind 对应的 GVR
func buildDynamic(client kubernetes.Interface, cfg *rest.Config, kind string) (dynamic.Interface, schema.GroupVersionResource, bool, error) {
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, schema.GroupVersionResource{}, false, fmt.Errorf("创建 dynamic client 失败: %w", err)
	}

	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, schema.GroupVersionResource{}, false, fmt.Errorf("创建 discovery client 失败: %w", err)
	}

	groups, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		return nil, schema.GroupVersionResource{}, false, fmt.Errorf("获取 API groups 失败: %w", err)
	}

	rm := restmapper.NewDiscoveryRESTMapper(groups)
	mappings, err := rm.RESTMappings(schema.GroupKind{Kind: kind})
	if err != nil || len(mappings) == 0 {
		return nil, schema.GroupVersionResource{}, false, fmt.Errorf("找不到 Kind %q 的 REST mapping", kind)
	}

	mapping := mappings[0]
	namespaced := mapping.Scope.Name() == "namespace"
	return dynClient, mapping.Resource, namespaced, nil
}

func describeResourceImpl(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, cluster, namespace, kind, name string) string {
	dynClient, gvr, namespaced, err := buildDynamic(client, cfg, kind)
	if err != nil {
		return toolError(err.Error())
	}

	var obj *unstructured.Unstructured
	if namespaced {
		obj, err = dynClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = dynClient.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return toolError(fmt.Sprintf("get resource 失败: %v", err))
	}

	// 移除噪音字段
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(obj.Object, "metadata", "annotations")

	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	status, _, _ := unstructured.NestedMap(obj.Object, "status")
	labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")

	age := ""
	if creationTime, ok, _ := unstructured.NestedString(obj.Object, "metadata", "creationTimestamp"); ok {
		if t, err := time.Parse(time.RFC3339, creationTime); err == nil {
			age = ageString(t)
		}
	}

	return toJSON(ResourceDetail{
		Cluster:   cluster,
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
		Age:       age,
		Labels:    labels,
		Spec:      spec,
		Status:    status,
	})
}

func applyManifestImpl(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, cluster, yamlContent string) string {
	// 解析 YAML/JSON 为 unstructured
	obj := &unstructured.Unstructured{}
	dec := k8syaml.NewYAMLOrJSONDecoder(strings.NewReader(yamlContent), 4096)
	if err := dec.Decode(&obj.Object); err != nil {
		return toolError(fmt.Sprintf("解析 YAML 失败: %v", err))
	}

	kind := obj.GetKind()
	if kind == "" {
		return toolError("YAML 中缺少 kind 字段")
	}

	dynClient, gvr, namespaced, err := buildDynamic(client, cfg, kind)
	if err != nil {
		return toolError(err.Error())
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return toolError(fmt.Sprintf("序列化对象失败: %v", err))
	}

	var result *unstructured.Unstructured
	force := true
	if namespaced {
		result, err = dynClient.Resource(gvr).Namespace(obj.GetNamespace()).Patch(
			ctx, obj.GetName(), types.ApplyPatchType, data,
			metav1.PatchOptions{FieldManager: "k8s-mcp", Force: &force},
		)
	} else {
		result, err = dynClient.Resource(gvr).Patch(
			ctx, obj.GetName(), types.ApplyPatchType, data,
			metav1.PatchOptions{FieldManager: "k8s-mcp", Force: &force},
		)
	}
	if err != nil {
		return toolError(fmt.Sprintf("apply 失败: %v", err))
	}

	return toJSON(map[string]interface{}{
		"cluster":     cluster,
		"kind":        result.GetKind(),
		"namespace":   result.GetNamespace(),
		"name":        result.GetName(),
		"api_version": result.GetAPIVersion(),
		"status":      "applied",
	})
}

func deleteResourceImpl(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, cluster, namespace, kind, name string) string {
	dynClient, gvr, namespaced, err := buildDynamic(client, cfg, kind)
	if err != nil {
		return toolError(err.Error())
	}

	if namespaced {
		err = dynClient.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	} else {
		err = dynClient.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{})
	}
	if err != nil {
		return toolError(fmt.Sprintf("delete resource 失败: %v", err))
	}

	return toJSON(map[string]interface{}{
		"cluster":   cluster,
		"kind":      kind,
		"namespace": namespace,
		"name":      name,
		"status":    "deleted",
	})
}
