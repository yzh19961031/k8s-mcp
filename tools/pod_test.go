package tools

import (
	"context"
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

	result := listPodsImpl(context.Background(), fc, "prod", "default", "", 20)

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

	result := describePodImpl(context.Background(), fc, "prod", "default", "web-1")

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

func TestGetPodLogsImpl(t *testing.T) {
	pod := makePod("web-1", "default", "Running", 1, 1, 0, "node-1")
	fc := fake.NewSimpleClientset(&pod)

	result := getPodLogsImpl(context.Background(), fc, "prod", "default", "web-1", "", 100, false)

	// fake client 返回空日志，但 JSON 结构应合法
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, result)
	}
	// 应包含必要字段
	for _, key := range []string{"cluster", "namespace", "pod", "tail", "logs"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected key %q in result", key)
		}
	}
}

func makePodWithStatus(name string, deletionTimestamp *metav1.Time, initStatuses []corev1.ContainerStatus, containerStatuses []corev1.ContainerStatus, phase string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			DeletionTimestamp: deletionTimestamp,
		},
		Status: corev1.PodStatus{
			Phase:                 corev1.PodPhase(phase),
			InitContainerStatuses: initStatuses,
			ContainerStatuses:     containerStatuses,
		},
	}
}

func TestPodDisplayStatus(t *testing.T) {
	now := metav1.Now()

	cases := []struct {
		name     string
		pod      corev1.Pod
		expected string
	}{
		{
			name:     "terminating",
			pod:      makePodWithStatus("p", &now, nil, nil, "Running"),
			expected: "Terminating",
		},
		{
			name: "crash loop backoff",
			pod: makePodWithStatus("p", nil, nil, []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			}, "Running"),
			expected: "CrashLoopBackOff",
		},
		{
			name: "image pull backoff",
			pod: makePodWithStatus("p", nil, nil, []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
					},
				},
			}, "Pending"),
			expected: "ImagePullBackOff",
		},
		{
			name: "oom killed",
			pod: makePodWithStatus("p", nil, nil, []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 137,
							Reason:   "OOMKilled",
						},
					},
				},
			}, "Failed"),
			expected: "OOMKilled",
		},
		{
			name: "init crash loop",
			pod: makePodWithStatus("p", nil, []corev1.ContainerStatus{
				{
					RestartCount: 3,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			}, nil, "Pending"),
			expected: "Init:CrashLoopBackOff",
		},
		{
			name: "running normally",
			pod: makePodWithStatus("p", nil, nil, []corev1.ContainerStatus{
				{Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			}, "Running"),
			expected: "Running",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := podDisplayStatus(tc.pod)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestGetPodLogsImpl_Previous(t *testing.T) {
	pod := makePod("web-1", "default", "Running", 1, 1, 0, "node-1")
	fc := fake.NewSimpleClientset(&pod)

	result := getPodLogsImpl(context.Background(), fc, "prod", "default", "web-1", "", 50, true)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, result)
	}
	if _, ok := m["logs"]; !ok {
		t.Error("expected 'logs' key in result")
	}
}
