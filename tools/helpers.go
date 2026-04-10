package tools

import (
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
)

// ageString 将时间点转换为人类可读的相对时间字符串
func ageString(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// toJSON 将任意结构体序列化为 JSON 字符串，序列化失败时返回错误描述
func toJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return toolError("json marshal failed: " + err.Error())
	}
	return string(b)
}

// toolError 生成标准错误 JSON
func toolError(msg string) string {
	return fmt.Sprintf(`{"error":%q}`, msg)
}

// mustString 从 args 中提取必填字符串参数，为空或类型错误时返回 error
func mustString(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("参数 %s 无效", key)
	}
	return v, nil
}

// getClusterClient 从 args 提取 cluster 参数并获取对应的 kubernetes.Interface
func getClusterClient(args map[string]any, mgr ClusterManagerInterface) (kubernetes.Interface, string, error) {
	cluster, err := mustString(args, "cluster")
	if err != nil {
		return nil, "", err
	}
	client, err := mgr.Get(cluster)
	if err != nil {
		return nil, "", err
	}
	return client, cluster, nil
}
