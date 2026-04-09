package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListNamespacesImpl(t *testing.T) {
	ns1 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "default",
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-24 * time.Hour)},
		},
		Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	ns2 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "kube-system",
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-48 * time.Hour)},
		},
		Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}

	fc := fake.NewSimpleClientset(&ns1, &ns2)
	result := listNamespacesImpl(context.Background(), fc, "prod")

	var r NamespaceListResult
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
