package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func makeDeployment(name, ns string, desired, ready int32) appsv1.Deployment {
	return appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-2 * time.Hour)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desired,
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:     ready,
			AvailableReplicas: ready,
			UpdatedReplicas:   desired,
		},
	}
}

func TestListDeploymentsImpl(t *testing.T) {
	d1 := makeDeployment("nginx", "default", 3, 3)
	d2 := makeDeployment("api", "default", 2, 1)
	fc := fake.NewSimpleClientset(&d1, &d2)

	result := listDeploymentsImpl(context.Background(), fc, "prod", "default")

	var r DeploymentListResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if r.Total != 2 {
		t.Errorf("expected 2, got %d", r.Total)
	}
	if r.Cluster != "prod" {
		t.Errorf("expected cluster=prod, got %s", r.Cluster)
	}
}
