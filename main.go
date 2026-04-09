package main

import (
	"flag"
	"log"

	"github.com/mark3labs/mcp-go/server"
	"github.com/yuanzhihao/k8s-mcp/k8s"
	"github.com/yuanzhihao/k8s-mcp/tools"
)

func main() {
	kubeconfigDir := flag.String("kubeconfig-dir", "", "kubeconfig 文件目录（可选，文件名即集群名）")
	tokenConfig := flag.String("token-config", "", "token 配置文件路径（可选，同名集群优先于 kubeconfig）")
	flag.Parse()

	mgr, err := k8s.NewClusterManager(*kubeconfigDir, *tokenConfig)
	if err != nil {
		log.Fatalf("初始化集群管理器失败: %v", err)
	}

	for name, errMsg := range mgr.ListErrors() {
		log.Printf("警告: 集群 %q 连接失败: %s", name, errMsg)
	}

	s := server.NewMCPServer("k8s-mcp", "1.0.0",
		server.WithToolCapabilities(true),
	)

	tools.RegisterClusterTools(s, mgr)
	tools.RegisterPodTools(s, mgr)
	tools.RegisterNamespaceTools(s, mgr)
	tools.RegisterNodeTools(s, mgr)
	tools.RegisterDeploymentTools(s, mgr)
	tools.RegisterEventTools(s, mgr)
	tools.RegisterResourceTools(s, mgr)

	log.Printf("k8s-mcp server 启动，已加载集群: %v", mgr.ListNames())

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("MCP Server 异常退出: %v", err)
	}
}
