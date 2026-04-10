package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/yuanzhihao/k8s-mcp/k8s"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

// noisyAnnotations 是描述资源时需要过滤掉的高噪音 annotation key
var noisyAnnotations = []string{
	"kubectl.kubernetes.io/last-applied-configuration",
	"deployment.kubernetes.io/revision",
}

// wellKnownGroups 为常见资源类型预设 API Group，避免 discovery mapper 因 Group 为空而匹配失败
var wellKnownGroups = map[string]string{
	"deployment":              "apps",
	"replicaset":              "apps",
	"statefulset":             "apps",
	"daemonset":               "apps",
	"controllerrevision":      "apps",
	"job":                     "batch",
	"cronjob":                 "batch",
	"ingress":                 "networking.k8s.io",
	"networkpolicy":           "networking.k8s.io",
	"horizontalpodautoscaler": "autoscaling",
	"poddisruptionbudget":     "policy",
	"clusterrole":             "rbac.authorization.k8s.io",
	"clusterrolebinding":      "rbac.authorization.k8s.io",
	"role":                    "rbac.authorization.k8s.io",
	"rolebinding":             "rbac.authorization.k8s.io",
	"storageclass":            "storage.k8s.io",
	"volumeattachment":        "storage.k8s.io",
}

// resolveGVR 通过 RESTMapper 将 kind 字符串解析为 GVR 和是否 namespaced
func resolveGVR(mapper meta.RESTMapper, kind string) (schema.GroupVersionResource, bool, error) {
	group := wellKnownGroups[strings.ToLower(kind)]
	mappings, err := mapper.RESTMappings(schema.GroupKind{Group: group, Kind: kind})
	if err != nil || len(mappings) == 0 {
		return schema.GroupVersionResource{}, false, fmt.Errorf("找不到 Kind %q 的 REST mapping", kind)
	}
	mapping := mappings[0]
	return mapping.Resource, mapping.Scope.Name() == "namespace", nil
}

// RegisterResourceTools 注册资源操作 MCP 工具
func RegisterResourceTools(s *server.MCPServer, cache *k8s.DynamicClientCache) {
	// list_resources
	s.AddTool(mcp.NewTool("list_resources",
		mcp.WithDescription("列出任意 K8s 资源（通用）。支持 Service/Ingress/PersistentVolumeClaim/ConfigMap/StatefulSet/DaemonSet 等所有 Kind"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Service、Ingress、PersistentVolumeClaim、ConfigMap")),
		mcp.WithString("namespace", mcp.Description("命名空间，留空查所有命名空间")),
		mcp.WithString("label_selector", mcp.Description("标签过滤，如 app=nginx")),
		mcp.WithNumber("limit", mcp.Description("返回数量上限，默认 20")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, err := mustString(args, "cluster")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		kind, err := mustString(args, "kind")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, _ := args["namespace"].(string)
		labelSelector, _ := args["label_selector"].(string)
		limit := 20
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		entry, err := cache.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(listResourcesImpl(ctx, entry, cluster, kind, namespace, labelSelector, limit)), nil
	})

	// describe_resource
	s.AddTool(mcp.NewTool("describe_resource",
		mcp.WithDescription("获取任意 K8s 资源的详细信息（通用）"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("命名空间，集群级资源传空字符串")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("资源类型，如 Deployment、Service、ConfigMap")),
		mcp.WithString("name", mcp.Required(), mcp.Description("资源名称")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, err := mustString(args, "cluster")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, _ := args["namespace"].(string)
		kind, err := mustString(args, "kind")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		name, err := mustString(args, "name")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		entry, err := cache.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(describeResourceImpl(ctx, entry, cluster, namespace, kind, name)), nil
	})

	// apply_manifest
	s.AddTool(mcp.NewTool("apply_manifest",
		mcp.WithDescription("应用 YAML 清单到指定集群（server-side apply）"),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("集群名称")),
		mcp.WithString("yaml_content", mcp.Required(), mcp.Description("YAML 或 JSON 格式的资源清单")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		cluster, err := mustString(args, "cluster")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		yamlContent, err := mustString(args, "yaml_content")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		entry, err := cache.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(applyManifestImpl(ctx, entry, cluster, yamlContent)), nil
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
		cluster, err := mustString(args, "cluster")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		namespace, _ := args["namespace"].(string)
		kind, err := mustString(args, "kind")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		name, err := mustString(args, "name")
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		entry, err := cache.Get(cluster)
		if err != nil {
			return mcp.NewToolResultText(toolError(err.Error())), nil
		}
		return mcp.NewToolResultText(deleteResourceImpl(ctx, entry, cluster, namespace, kind, name)), nil
	})
}

func listResourcesImpl(ctx context.Context, entry *k8s.DynamicEntry, cluster, kind, namespace, labelSelector string, limit int) string {
	gvr, namespaced, err := resolveGVR(entry.Mapper, kind)
	if err != nil {
		return toolError(err.Error())
	}

	opts := metav1.ListOptions{LabelSelector: labelSelector, Limit: int64(limit)}

	var list *unstructured.UnstructuredList
	if namespaced {
		list, err = entry.Client.Resource(gvr).Namespace(namespace).List(ctx, opts)
	} else {
		list, err = entry.Client.Resource(gvr).List(ctx, opts)
	}
	if err != nil {
		return toolError(fmt.Sprintf("list %s 失败: %v", kind, err))
	}

	items := make([]ResourceBrief, 0, len(list.Items))
	for _, obj := range list.Items {
		age := ""
		ts := obj.GetCreationTimestamp()
		if !ts.IsZero() {
			age = ageString(ts.Time)
		}
		items = append(items, ResourceBrief{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Age:       age,
			Labels:    obj.GetLabels(),
		})
	}

	return toJSON(ResourceListResult{
		Cluster:   cluster,
		Kind:      kind,
		Namespace: namespace,
		Total:     len(items),
		Items:     items,
	})
}

func describeResourceImpl(ctx context.Context, entry *k8s.DynamicEntry, cluster, namespace, kind, name string) string {
	gvr, namespaced, err := resolveGVR(entry.Mapper, kind)
	if err != nil {
		return toolError(err.Error())
	}

	var obj *unstructured.Unstructured
	if namespaced {
		obj, err = entry.Client.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = entry.Client.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return toolError(fmt.Sprintf("get resource 失败: %v", err))
	}

	// 移除 managedFields 噪音
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")

	// 过滤高噪音 annotation，保留业务 annotation（如 hami.io/*）
	if annots, ok, _ := unstructured.NestedStringMap(obj.Object, "metadata", "annotations"); ok {
		for _, key := range noisyAnnotations {
			delete(annots, key)
		}
		_ = unstructured.SetNestedStringMap(obj.Object, annots, "metadata", "annotations")
	}

	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	status, _, _ := unstructured.NestedMap(obj.Object, "status")
	labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
	annots, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")

	age := ""
	if creationTime, ok, _ := unstructured.NestedString(obj.Object, "metadata", "creationTimestamp"); ok {
		if t, err := time.Parse(time.RFC3339, creationTime); err == nil {
			age = ageString(t)
		}
	}

	return toJSON(map[string]interface{}{
		"cluster":     cluster,
		"kind":        kind,
		"namespace":   namespace,
		"name":        name,
		"age":         age,
		"labels":      labels,
		"annotations": annots,
		"spec":        spec,
		"status":      status,
	})
}

func applyManifestImpl(ctx context.Context, entry *k8s.DynamicEntry, cluster, yamlContent string) string {
	obj := &unstructured.Unstructured{}
	dec := k8syaml.NewYAMLOrJSONDecoder(strings.NewReader(yamlContent), 4096)
	if err := dec.Decode(&obj.Object); err != nil {
		return toolError(fmt.Sprintf("解析 YAML 失败: %v", err))
	}

	kind := obj.GetKind()
	if kind == "" {
		return toolError("YAML 中缺少 kind 字段")
	}

	gvr, namespaced, err := resolveGVR(entry.Mapper, kind)
	if err != nil {
		return toolError(err.Error())
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return toolError(fmt.Sprintf("序列化对象失败: %v", err))
	}

	force := true
	var result *unstructured.Unstructured
	if namespaced {
		result, err = entry.Client.Resource(gvr).Namespace(obj.GetNamespace()).Patch(
			ctx, obj.GetName(), types.ApplyPatchType, data,
			metav1.PatchOptions{FieldManager: "k8s-mcp", Force: &force},
		)
	} else {
		result, err = entry.Client.Resource(gvr).Patch(
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

func deleteResourceImpl(ctx context.Context, entry *k8s.DynamicEntry, cluster, namespace, kind, name string) string {
	gvr, namespaced, err := resolveGVR(entry.Mapper, kind)
	if err != nil {
		return toolError(err.Error())
	}

	if namespaced {
		err = entry.Client.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	} else {
		err = entry.Client.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{})
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
