package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func makeNode(name string, labels map[string]string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Labels:            labels,
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-72 * time.Hour)},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
			NodeInfo: corev1.NodeSystemInfo{
				KubeletVersion:  "v1.29.0",
				OperatingSystem: "linux",
				Architecture:    "amd64",
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			},
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.10"},
			},
		},
	}
}

func TestListNodesImpl(t *testing.T) {
	node := makeNode("worker-1", map[string]string{"node-role.kubernetes.io/worker": ""})
	fc := fake.NewSimpleClientset(&node)

	result := listNodesImpl(context.Background(), fc, "prod")

	var r NodeListResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if r.Total != 1 {
		t.Errorf("expected 1 node, got %d", r.Total)
	}
	if r.Items[0].Status != "Ready" {
		t.Errorf("expected status=Ready, got %s", r.Items[0].Status)
	}
	if r.Items[0].Roles != "worker" {
		t.Errorf("expected roles=worker, got %s", r.Items[0].Roles)
	}
}

func TestDescribeNodeImpl(t *testing.T) {
	node := makeNode("master-1", map[string]string{"node-role.kubernetes.io/control-plane": ""})
	fc := fake.NewSimpleClientset(&node)

	result := describeNodeImpl(context.Background(), fc, "prod", "master-1")

	var r NodeDetail
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if r.Name != "master-1" {
		t.Errorf("expected name=master-1, got %s", r.Name)
	}
	if r.Roles != "control-plane" {
		t.Errorf("expected roles=control-plane, got %s", r.Roles)
	}
	if r.Status != "Ready" {
		t.Errorf("expected status=Ready, got %s", r.Status)
	}
}
