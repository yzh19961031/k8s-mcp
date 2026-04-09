package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/rest"
)

// TokenCluster 表示 token 方式接入的一个集群配置
type TokenCluster struct {
	Name                  string `yaml:"name"`
	Server                string `yaml:"server"`
	Token                 string `yaml:"token"`
	InsecureSkipTLSVerify bool   `yaml:"insecure_skip_tls_verify"`
}

type tokenConfigFile struct {
	Clusters []TokenCluster `yaml:"clusters"`
}

// LoadTokenConfig 从 YAML 文件加载 token 集群配置
func LoadTokenConfig(path string) ([]TokenCluster, error) {
	expanded := expandHome(path)
	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("读取 token 配置文件失败 %q: %w", path, err)
	}
	var cfg tokenConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析 token 配置文件失败: %w", err)
	}
	for i, c := range cfg.Clusters {
		if c.Name == "" {
			return nil, fmt.Errorf("第 %d 个集群缺少 name 字段", i+1)
		}
		if c.Server == "" {
			return nil, fmt.Errorf("集群 %q 缺少 server 字段", c.Name)
		}
		if c.Token == "" {
			return nil, fmt.Errorf("集群 %q 缺少 token 字段", c.Name)
		}
	}
	return cfg.Clusters, nil
}

// TokenClusterToRestConfig 将 TokenCluster 转换为 rest.Config
func TokenClusterToRestConfig(tc TokenCluster) *rest.Config {
	return &rest.Config{
		Host:        tc.Server,
		BearerToken: tc.Token,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: tc.InsecureSkipTLSVerify,
		},
	}
}
