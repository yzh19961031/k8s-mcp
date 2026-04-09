package tools

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func makePod(name, ns, phase string, ready, total int32, restarts int32, nodeName string) corev1.Pod {
	containerStatuses := make([]corev1.ContainerStatus, total)
	for i := int32(0); i < total; i++ {
		containerStatuses[i] = corev1.ContainerStatus{
			Ready:        i < ready,
			RestartCount: restarts,
			Name:         fmt.Sprintf("container-%d", i),
		}
	}
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
		},
		Spec: corev1.PodSpec{NodeName: nodeName},
		Status: corev1.PodStatus{
			Phase:             corev1.PodPhase(phase),
			ContainerStatuses: containerStatuses,
		},
	}
}

func TestListPodsImpl(t *testing.T) {
	pod1 := makePod("web-1", "default", "Running", 1, 1, 0, "node-1")
	pod2 := makePod("web-2", "default", "Pending", 0, 1, 2, "")

	fc := fake.NewSimpleClientset(&pod1, &pod2)

	result := listPodsImpl(fc, "prod", "default", "", 20)

	var r PodListResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if r.Cluster != "prod" {
		t.Errorf("expected cluster=prod, got %s", r.Cluster)
	}
	if r.Total != 2 {
		t.Errorf("expected total=2, got %d", r.Total)
	}
}

func TestDescribePodImpl(t *testing.T) {
	pod := makePod("web-1", "default", "Running", 1, 1, 0, "node-1")
	fc := fake.NewSimpleClientset(&pod)

	result := describePodImpl(fc, "prod", "default", "web-1")

	var r PodDetail
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if r.Name != "web-1" {
		t.Errorf("expected name=web-1, got %s", r.Name)
	}
	if r.Cluster != "prod" {
		t.Errorf("expected cluster=prod, got %s", r.Cluster)
	}
}
