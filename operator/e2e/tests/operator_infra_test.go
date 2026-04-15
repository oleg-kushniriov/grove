//go:build e2e

// /*
// Copyright 2026 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/ai-dynamo/grove/operator/e2e/testctx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test_OI1_OperatorIgnoresUnmanagedPods verifies that the operator's filtered pod cache
// causes it to ignore pods without the grove managed-by label.
// Scenario OI-1:
//  1. Initialize a 10-node Grove cluster
//  2. Deploy workload WL1 and verify 10 managed pods are running
//  3. Create an unmanaged pod (no grove labels) in the same namespace
//  4. Wait and verify the operator does not modify the unmanaged pod
func Test_OI1_OperatorIgnoresUnmanagedPods(t *testing.T) {
	ctx := context.Background()

	Logger.Info("1. Initialize a 10-node Grove cluster")
	expectedManagedPods := 10
	tc, cleanup := testctx.PrepareTest(ctx, t, 10,
		testctx.WithTimeout(5*time.Minute),
		testctx.WithWorkload(&testctx.WorkloadConfig{
			Name:         "workload1",
			YAMLPath:     "../yaml/workload1.yaml",
			Namespace:    "default",
			ExpectedPods: expectedManagedPods,
		}),
	)
	defer cleanup()

	Logger.Info("2. Deploy workload WL1, and verify 10 managed pods are running")
	_, err := tc.DeployAndVerifyWorkload()
	require.NoError(t, err, "Failed to deploy workload")

	err = tc.WaitForReadyPods(expectedManagedPods)
	require.NoError(t, err, "Failed to wait for managed pods to be ready")

	Logger.Info("3. Create an unmanaged pod (no grove labels) in the same namespace")
	unmanagedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unmanaged-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app": "not-grove",
			},
		},
		Spec: corev1.PodSpec{
			// Use the same tolerations and affinity as workload pods so it can schedule
			Tolerations: []corev1.Toleration{
				{
					Key:      "node_role.e2e.grove.nvidia.com",
					Operator: corev1.TolerationOpEqual,
					Value:    "agent",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "registry:5001/busybox:latest",
					Command: []string{"sleep", "30"},
				},
			},
		},
	}

	_, err = tc.Clients.Clientset.CoreV1().Pods("default").Create(ctx, unmanagedPod, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create unmanaged pod")

	// Clean up the unmanaged pod when done
	defer func() {
		_ = tc.Clients.Clientset.CoreV1().Pods("default").Delete(ctx, "unmanaged-pod", metav1.DeleteOptions{})
	}()

	Logger.Info("4. Verify the operator does not modify the unmanaged pod")
	// Wait briefly to give the operator a chance to act (it shouldn't)
	time.Sleep(10 * time.Second)

	pod, err := tc.Clients.Clientset.CoreV1().Pods("default").Get(ctx, "unmanaged-pod", metav1.GetOptions{})
	require.NoError(t, err, "Failed to get unmanaged pod")

	// The operator should not have added any owner references
	assert.Empty(t, pod.OwnerReferences, "unmanaged pod should have no owner references")

	// The operator should not have added grove labels
	_, hasManagedBy := pod.Labels["app.kubernetes.io/managed-by"]
	assert.False(t, hasManagedBy, "unmanaged pod should not have managed-by label")

	_, hasPartOf := pod.Labels["app.kubernetes.io/part-of"]
	assert.False(t, hasPartOf, "unmanaged pod should not have part-of label")

	// The operator should not have added grove finalizers
	assert.Empty(t, pod.Finalizers, "unmanaged pod should have no finalizers")

	// Verify managed pods are still healthy (operator is still working)
	managedPods, err := tc.ListPods()
	require.NoError(t, err, "Failed to list managed pods")
	assert.Len(t, managedPods.Items, expectedManagedPods, "managed pod count should be unchanged")
	for _, p := range managedPods.Items {
		assert.NotEqual(t, "unmanaged-pod", p.Name, "unmanaged pod should not appear in managed pod list")
	}

	Logger.Info("Pod cache filtering test completed successfully!")
}
