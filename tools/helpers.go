package tools

import (
	"encoding/json"
	"fmt"
	"time"
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
