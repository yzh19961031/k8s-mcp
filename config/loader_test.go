package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKubeconfigs(t *testing.T) {
	// 创建临时目录，放两个假 yaml 文件
	dir := t.TempDir()
	for _, name := range []string{"prod-gpu.yaml", "dev-gpu.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fake"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// 大写扩展名测试
	if err := os.WriteFile(filepath.Join(dir, "staging.YAML"), []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}
	// 放一个非 yaml 文件，不应被加载
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := LoadKubeconfigs(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 clusters, got %d", len(result))
	}
	if _, ok := result["prod-gpu"]; !ok {
		t.Error("expected cluster 'prod-gpu'")
	}
	if _, ok := result["dev-gpu"]; !ok {
		t.Error("expected cluster 'dev-gpu'")
	}
	// 验证大写扩展名被正确去除
	if _, ok := result["staging"]; !ok {
		t.Error("expected cluster 'staging' (from staging.YAML)")
	}
}

func TestLoadKubeconfigs_DirNotExist(t *testing.T) {
	_, err := LoadKubeconfigs("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}
