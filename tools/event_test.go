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

func TestGetEventsImpl(t *testing.T) {
	lastTime := metav1.Time{Time: time.Now().Add(-1 * time.Minute)}
	ev := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pod-crash.1234",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
		},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-1"},
		Reason:         "BackOff",
		Message:        "Back-off restarting failed container",
		Type:           "Warning",
		Count:          5,
		LastTimestamp:  lastTime,
	}

	fc := fake.NewSimpleClientset(&ev)
	result := getEventsImpl(context.Background(), fc, "prod", "default", 20)

	var r EventListResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if r.Total != 1 {
		t.Errorf("expected 1 event, got %d", r.Total)
	}
	if r.Items[0].Type != "Warning" {
		t.Errorf("expected Warning event, got %s", r.Items[0].Type)
	}
	if r.Items[0].LastTimestamp == "" {
		t.Error("expected LastTimestamp to be set")
	}
}
