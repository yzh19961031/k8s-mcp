package main

import (
	"flag"
	"log"

	"github.com/mark3labs/mcp-go/server"
	"github.com/yuanzhihao/k8s-mcp/k8s"
	"github.com/yuanzhihao/k8s-mcp/tools"
)

func main() {
	kubeconfigDir := flag.String("kubeconfig-dir", "~/.kube/clusters", "kubeconfig 文件目录")
	flag.Parse()

	mgr, err := k8s.NewClusterManager(*kubeconfigDir)
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

	log.Printf("k8s-mcp server 启动，已加载集群: %v", mgr.ListNames())

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("MCP Server 异常退出: %v", err)
	}
}
