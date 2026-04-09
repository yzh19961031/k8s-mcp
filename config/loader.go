package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadKubeconfigs 扫描指定目录下的 .yaml/.yml 文件，
// 返回 map[clusterName]absoluteFilePath
// 文件名（不含扩展名）即为集群名
func LoadKubeconfigs(dir string) (map[string]string, error) {
	expanded := expandHome(dir)
	entries, err := os.ReadDir(expanded)
	if err != nil {
		return nil, fmt.Errorf("读取 kubeconfig 目录失败 %q: %w", dir, err)
	}

	result := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		originalExt := filepath.Ext(name)
		ext := strings.ToLower(originalExt)
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		clusterName := strings.TrimSuffix(name, originalExt)
		result[clusterName] = filepath.Join(expanded, name)
	}
	return result, nil
}

// expandHome 将路径中的 ~ 前缀替换为当前用户的 home 目录。
func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
