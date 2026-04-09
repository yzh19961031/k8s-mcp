package tools

// ClusterInfo 单个集群状态
type ClusterInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok" | "error"
	Error  string `json:"error,omitempty"`
}

// ClusterListResult list_clusters 返回值
type ClusterListResult struct {
	Total int           `json:"total"`
	Items []ClusterInfo `json:"items"`
}

// NamespaceBrief 命名空间摘要
type NamespaceBrief struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Age    string `json:"age"`
}

// NamespaceListResult list_namespaces 返回值
type NamespaceListResult struct {
	Cluster string           `json:"cluster"`
	Total   int              `json:"total"`
	Items   []NamespaceBrief `json:"items"`
}

// NodeBrief 节点摘要
type NodeBrief struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Roles    string `json:"roles"`
	Age      string `json:"age"`
	Version  string `json:"version"`
	CPUUsage string `json:"cpu_usage,omitempty"`
	MemUsage string `json:"mem_usage,omitempty"`
}

// NodeListResult list_nodes 返回值
type NodeListResult struct {
	Cluster string      `json:"cluster"`
	Total   int         `json:"total"`
	Items   []NodeBrief `json:"items"`
}

// NodeDetail describe_node 返回值
type NodeDetail struct {
	Cluster     string            `json:"cluster"`
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	Roles       string            `json:"roles"`
	Age         string            `json:"age"`
	Version     string            `json:"version"`
	OS          string            `json:"os"`
	Arch        string            `json:"arch"`
	Addresses   []string          `json:"addresses"`
	Capacity    map[string]string `json:"capacity"`
	Allocatable map[string]string `json:"allocatable"`
	Conditions  []string          `json:"conditions"`
}

// PodBrief Pod 摘要
type PodBrief struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	Ready     string `json:"ready"`    // "2/3"
	Restarts  int32  `json:"restarts"`
	Age       string `json:"age"`
	NodeName  string `json:"node_name"`
}

// PodListResult list_pods 返回值
type PodListResult struct {
	Cluster   string     `json:"cluster"`
	Namespace string     `json:"namespace"`
	Total     int        `json:"total"`
	Items     []PodBrief `json:"items"`
}

// PodDetail describe_pod 返回值
type PodDetail struct {
	Cluster    string            `json:"cluster"`
	Namespace  string            `json:"namespace"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	NodeName   string            `json:"node_name"`
	Age        string            `json:"age"`
	Labels     map[string]string `json:"labels,omitempty"`
	Containers []ContainerInfo   `json:"containers"`
	Conditions []string          `json:"conditions"`
}

// ContainerInfo 容器摘要
type ContainerInfo struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`
	State    string `json:"state"`
}

// DeploymentBrief Deployment 摘要
type DeploymentBrief struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"`    // "2/3"
	UpToDate  int32  `json:"up_to_date"`
	Available int32  `json:"available"`
	Age       string `json:"age"`
}

// DeploymentListResult list_deployments 返回值
type DeploymentListResult struct {
	Cluster   string            `json:"cluster"`
	Namespace string            `json:"namespace"`
	Total     int               `json:"total"`
	Items     []DeploymentBrief `json:"items"`
}

// EventBrief 事件摘要
type EventBrief struct {
	Namespace     string `json:"namespace"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Type          string `json:"type"` // Normal | Warning
	Count         int32  `json:"count"`
	Age           string `json:"age"`
	LastTimestamp string `json:"last_timestamp,omitempty"`
}

// EventListResult get_events 返回值
type EventListResult struct {
	Cluster   string       `json:"cluster"`
	Namespace string       `json:"namespace"`
	Total     int          `json:"total"`
	Items     []EventBrief `json:"items"`
}

// ReplicaSetBrief RS 摘要
type ReplicaSetBrief struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Desired   int32  `json:"desired"`
	Ready     int32  `json:"ready"`
	Available int32  `json:"available"`
	Age       string `json:"age"`
}

// ReplicaSetListResult list_replicasets 返回值
type ReplicaSetListResult struct {
	Cluster   string           `json:"cluster"`
	Namespace string           `json:"namespace"`
	Total     int              `json:"total"`
	Items     []ReplicaSetBrief `json:"items"`
}

// ResourceQuotaBrief ResourceQuota 摘要
type ResourceQuotaBrief struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Age       string            `json:"age"`
	Hard      map[string]string `json:"hard"`
	Used      map[string]string `json:"used"`
}

// ResourceQuotaListResult list_resource_quotas 返回值
type ResourceQuotaListResult struct {
	Cluster   string               `json:"cluster"`
	Namespace string               `json:"namespace"`
	Total     int                  `json:"total"`
	Items     []ResourceQuotaBrief `json:"items"`
}

// ResourceDetail describe_resource 返回值
type ResourceDetail struct {
	Cluster   string                 `json:"cluster"`
	Kind      string                 `json:"kind"`
	Namespace string                 `json:"namespace"`
	Name      string                 `json:"name"`
	Age       string                 `json:"age"`
	Labels    map[string]string      `json:"labels,omitempty"`
	Spec      map[string]interface{} `json:"spec,omitempty"`
	Status    map[string]interface{} `json:"status,omitempty"`
}
