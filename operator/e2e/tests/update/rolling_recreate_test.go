//go:build e2e

// /*
// Copyright 2025 The Grove Authors.
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

package update

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ai-dynamo/grove/operator/e2e/k8s"
	"k8s.io/apimachinery/pkg/watch"
)

// Test_RU7_RollingUpdatePCSPodClique tests rolling update when PCS-owned Podclique spec is updated
// Scenario RU-7:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Change the specification of pc-a
// 4. Verify that only one pod is deleted at a time
// 5. Verify that a single PCS replica is updated first before moving to another
func Test_RU7_RollingUpdatePCSPodClique(t *testing.T) {
	logger.Info("1. Initialize a 10-node Grove cluster")
	logger.Info("2. Deploy workload WL1, and verify 10 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:  10,
		ExpectedPods: 10,
	})
	defer cleanup()

	logger.Info("3. Change the specification of pc-a")
	if err := ts.triggerPodCliqueRollingUpdate("pc-a"); err != nil {
		t.Fatalf("Failed to update PodClique spec: %v", err)
	}

	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 1 * time.Minute
	if err := tsLongTimeout.waitForRollingUpdateComplete(1); err != nil {
		// Diagnostics will be collected automatically by cleanup on test failure
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tests.Logger.Info("4. Verify that only one pod is deleted at a time")
	tracker.stop()
	events := tracker.getEvents()
	ts.verifyOnePodDeletedAtATime(events)

	logger.Info("5. Verify that a single PCS replica is updated first before moving to another")
	ts.verifySinglePCSReplicaUpdatedFirst(events)

	tests.Logger.Info("Rolling Update on PCS-owned Podclique test (RU-7) completed successfully!")
}

// Test_RU8_RollingUpdatePCSGPodClique tests rolling update when PCSG-owned Podclique spec is updated
// Scenario RU-8:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Change the specification of pc-b
// 4. Verify that only one PCSG replica is deleted at a time
// 5. Verify that a single PCS replica is updated first before moving to another
func Test_RU8_RollingUpdatePCSGPodClique(t *testing.T) {
	logger.Info("1. Initialize a 10-node Grove cluster")
	logger.Info("2. Deploy workload WL1, and verify 10 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:  10,
		ExpectedPods: 10,
	})
	defer cleanup()

	logger.Info("3. Change the specification of pc-b")
	if err := ts.triggerPodCliqueRollingUpdate("pc-b"); err != nil {
		t.Fatalf("Failed to update PodClique spec: %v", err)
	}

	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 1 * time.Minute
	if err := tsLongTimeout.waitForRollingUpdateComplete(1); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tests.Logger.Info("4. Verify that only one PCSG replica is deleted at a time")
	tracker.stop()
	events := tracker.getEvents()
	ts.verifyOnePCSGReplicaDeletedAtATime(events)

	logger.Info("5. Verify that a single PCS replica is updated first before moving to another")
	ts.verifySinglePCSReplicaUpdatedFirst(events)

	tests.Logger.Info("Rolling Update on PCSG-owned Podclique test (RU-8) completed successfully!")
}

// Test_RU9_RollingUpdateAllPodCliques tests rolling update when all Podclique specs are updated
// Scenario RU-9:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Verify that only one pod in each Podclique and one replica in each PCSG is deleted at a time
// 5. Verify that a single PCS replica is updated first before moving to another
func Test_RU9_RollingUpdateAllPodCliques(t *testing.T) {
	logger.Info("1. Initialize a 10-node Grove cluster")
	logger.Info("2. Deploy workload WL1, and verify 10 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:  10,
		ExpectedPods: 10,
	})
	defer cleanup()

	tests.Logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		if err := ts.triggerPodCliqueRollingUpdate(cliqueName); err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	if err := ts.waitForRollingUpdateComplete(1); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tests.Logger.Info("4. Verify that only one pod in each Podclique and one replica in each PCSG is deleted at a time")
	tracker.stop()
	events := tracker.getEvents()
	ts.verifySinglePCSReplicaUpdatedFirst(events)
	ts.verifyOnePodDeletedAtATimePerPodclique(events)
	ts.verifyOnePCSGReplicaDeletedAtATimePerPCSG(events)

	logger.Info("5. Verify that a single PCS replica is updated first before moving to another")
	ts.verifySinglePCSReplicaUpdatedFirst(events)

	tests.Logger.Info("Rolling Update on all Podcliques test (RU-9) completed successfully!")
}

// Test_RU10_RollingUpdateInsufficientResources tests rolling update behavior with insufficient resources.
// This test verifies the "delete-first" strategy: when nodes are cordoned, the operator deletes
// one old pod and creates a new one (which remains Pending). No further progress is made until
// nodes are uncordoned, at which point the rolling update completes.
// Scenario RU-10:
// 1. Initialize a 10-node Grove cluster
// 2. Deploy workload WL1, and verify 10 newly created pods
// 3. Cordon all worker nodes
// 4. Change the specification of pc-a
// 5. Verify exactly one pod is deleted and a new Pending pod is created (delete-first strategy)
// 6. Uncordon the nodes, and verify the rolling update completes
func Test_RU10_RollingUpdateInsufficientResources(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 10-node Grove cluster")
	ts, cleanup := prepareTest(ctx, t, 10,
		WithWorkload(&WorkloadConfig{
			Name:         "workload1",
			YAMLPath:     "../../yaml/workload1.yaml",
			Namespace:    "default",
			ExpectedPods: 10,
		}),
	)
	defer cleanup()

	logger.Info("2. Deploy workload WL1, and verify 10 newly created pods")
	_, err := ts.DeployAndVerifyWorkload()
	if err != nil {
		t.Fatalf("Failed to deploy workload: %v", err)
	}

	if err := ts.WaitForPods(10); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	logger.Info("3. Cordon all worker nodes")
	workerNodes, err := ts.GetWorkerNodes()
	if err != nil {
		t.Fatalf("Failed to get agent nodes: %v", err)
	}

	ts.CordonNodes(workerNodes)

	// Capture the existing pods before starting the tracker
	existingPods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list existing pods: %v", err)
	}

	// Capture the existing pods names for verification later
	existingPodNames := make(map[string]bool)
	for _, pod := range existingPods.Items {
		existingPodNames[pod.Name] = true
	}
	tests.Logger.Debugf("Captured %d existing pods before rolling update", len(existingPodNames))

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ts); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.stop()

	logger.Info("4. Change the specification of pc-a")
	if err := ts.triggerPodCliqueRollingUpdate("pc-a"); err != nil {
		t.Fatalf("Failed to update PodClique spec: %v", err)
	}

	tests.Logger.Info("5. Verify exactly one pod is deleted and a new Pending pod is created (delete-first strategy)")

	// Poll until we see exactly 1 pod deleted and 1 new pod created, verifying delete-first behavior
	pollErr := k8s.PollForCondition(ctx, 2*time.Minute, 2*time.Second, func() (bool, error) {
		events := tracker.getEvents()
		var deletedExistingPods []string
		var addedPods []string

		for _, event := range events {
			switch event.eventType {
			case watch.Deleted:
				if existingPodNames[event.pod.Name] {
					deletedExistingPods = append(deletedExistingPods, event.pod.Name)
					tests.Logger.Debugf("Existing pod deleted during rolling update: %s", event.pod.Name)
				}
			case watch.Added:
				if !existingPodNames[event.pod.Name] {
					addedPods = append(addedPods, event.pod.Name)
					tests.Logger.Debugf("New pod created during rolling update: %s", event.pod.Name)
				}
			}
		}

		// Check if we've reached the expected delete-first state
		deletedCount := len(deletedExistingPods)
		addedCount := len(addedPods)

		// If more than 1 pod was deleted, the test should fail (not delete-first)
		if deletedCount > 1 {
			return false, fmt.Errorf("rolling update progressed beyond first deletion - expected 1 pod deleted but got %d: %v (not delete-first strategy)", deletedCount, deletedExistingPods)
		}

		// Success: exactly 1 deleted and 1 new pod created
		if deletedCount == 1 && addedCount == 1 {
			tests.Logger.Infof("Delete-first strategy verified: 1 pod deleted (%v), 1 new pod created (%v)", deletedExistingPods, addedPods)
			return true, nil
		}

		// Still waiting for the condition
		tests.Logger.Debugf("Waiting for delete-first state: deleted=%d (want 1), added=%d (want 1)", deletedCount, addedCount)
		return false, nil
	})

	if pollErr != nil {
		t.Fatalf("Failed to verify delete-first strategy: %v", pollErr)
	}

	logger.Info("6. Uncordon the nodes, and verify the rolling update completes")
	ts.UncordonNodes(workerNodes)

	// Wait for rolling update to complete after uncordoning
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 5 * time.Minute
	if err := tsLongTimeout.waitForRollingUpdateComplete(1); err != nil {
		logger.Info("=== Rolling update timed out - capturing debug info ===")
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	tests.Logger.Info("Rolling Update with insufficient resources test (RU-10) completed successfully!")
}

// Test_RU11_RollingUpdateWithPCSScaleOut tests rolling update with scale-out on PCS
// Scenario RU-11:
// 1. Initialize a 30-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a
// 4. Scale out the PCS during the rolling update
// 5. Verify the scaled out replica is created with the correct specifications
func Test_RU11_RollingUpdateWithPCSScaleOut(t *testing.T) {
	logger.Info("1. Initialize a 30-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:        30,
		ExpectedPods:       10,
		PatchSIGTERM:       true,
		InitialPCSReplicas: 2,
		PostScalePods:      20,
	})
	defer cleanup()

	logger.Info("3. Change the specification of pc-a")
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 2 * time.Minute
	updateWait := tsLongTimeout.triggerRollingUpdate(3, "pc-a")

	logger.Info("4. Scale out the PCS during the rolling update (in parallel)")
	scaleWait := tsLongTimeout.ScalePCSAsync("workload1", 3, 30, 0, 100) // 100ms delay so update is "first"

	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	logger.Info("5. Verify the scaled out replica is created with the correct specifications")
	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 30 {
		t.Fatalf("Expected 30 pods, got %d", len(pods.Items))
	}

	tracker.stop()
	events := tracker.getEvents()
	tests.Logger.Debugf("Captured %d pod events during rolling update", len(events))

	tests.Logger.Info("Rolling Update with PCS scale-out test (RU-11) completed successfully!")
}

// Test_RU12_RollingUpdateWithPCSScaleInDuringUpdate tests rolling update with scale-in on PCS while final ordinal is being updated
// Scenario RU-12:
// 1. Initialize a 30-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale in the PCS while the final ordinal is being updated
// 5. Verify the update goes through successfully
func Test_RU12_RollingUpdateWithPCSScaleInDuringUpdate(t *testing.T) {
	logger.Info("1. Initialize a 30-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:        30,
		ExpectedPods:       10,
		PatchSIGTERM:       true,
		InitialPCSReplicas: 2,
		PostScalePods:      20,
	})
	defer cleanup()

	tests.Logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	// Use raw trigger since we need to wait for ordinal before starting the wait
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		if err := ts.triggerPodCliqueRollingUpdate(cliqueName); err != nil {
			t.Fatalf("Failed to trigger rolling update on %s: %v", cliqueName, err)
		}
	}

	tests.Logger.Info("4. Scale in the PCS while the final ordinal is being updated")
	// Wait for the final ordinal (ordinal 1 since there's two replicas, indexed from 0) to start updating before scaling in
	// Rolling updates process ordinals from highest to lowest, so ordinal 1 is updated first
	tsOrdinalTimeout := *ts
	tsOrdinalTimeout.Timeout = 60 * time.Second
	if err := tsOrdinalTimeout.waitForOrdinalUpdating(1); err != nil {
		t.Fatalf("Failed to wait for final ordinal to start updating: %v", err)
	}

	// Scale in parallel with the ongoing rolling update (no delay since we already waited for ordinal)
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 2 * time.Minute
	scaleWait := tsLongTimeout.ScalePCSAsync("workload1", 1, 10, 0, 0) // No delay since update already in progress

	logger.Info("5. Verify the update goes through successfully")
	updateWait := tsLongTimeout.waitForRollingUpdate(1)

	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 10 {
		t.Fatalf("Expected 10 pods, got %d", len(pods.Items))
	}
	tracker.stop()

	tests.Logger.Info("Rolling Update with PCS scale-in during update test (RU-12) completed successfully!")
}

/* This test is flaky. It sometimes fails with "Failed to wait for rolling update to complete: condition not met..."
// Test_RU13_RollingUpdateWithPCSScaleInAfterFinalOrdinal tests rolling update with scale-in on PCS after final ordinal finishes
// Scenario RU-13:
// 1. Initialize a 20-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Wait for rolling update to complete on replica 1
// 5. Scale in the PCS after final ordinal has been updated
// 6. Verify the update goes through successfully
func Test_RU13_RollingUpdateWithPCSScaleInAfterFinalOrdinal(t *testing.T) {
	logger.Info("1. Initialize a 20-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:        20,
		ExpectedPods:       10,
		InitialPCSReplicas: 2,
		PostScalePods:      20,
	})
	defer cleanup()

	tests.Logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	for _, cliqueName := range []string{"pc-a", "pc-b", "pc-c"} {
		if err := ts.triggerPodCliqueRollingUpdate(cliqueName); err != nil {
			t.Fatalf("Failed to update PodClique %s spec: %v", cliqueName, err)
		}
	}

	logger.Info("4. Wait for rolling update to complete on both replicas")
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 2 * time.Minute
	if err := tsLongTimeout.waitForRollingUpdateComplete(2); err != nil {
		t.Fatalf("Failed to wait for rolling update to complete: %v", err)
	}

	logger.Info("5. Scale in the PCS after final ordinal has been updated")
	ts.ScalePCSAndWait("workload1", 1, 10, 0)

	logger.Info("6. Verify the update goes through successfully")
	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 10 {
		t.Fatalf("Expected 10 pods, got %d", len(pods.Items))
	}
	tracker.Stop()

	tests.Logger.Info("Rolling Update with PCS scale-in after final ordinal test (RU-13) completed successfully!")
}
*/

/* This test is flaky. It sometimes fails with rolling_updates_test.go:454: Rolling update failed: condition not met within timeout
// Test_RU14_RollingUpdateWithPCSGScaleOutDuringUpdate tests rolling update with scale-out on PCSG being updated
// Scenario RU-14:
// 1. Initialize a 28-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale out the PCSG during its rolling update
// 5. Verify the scaled out replica is created with the correct specifications
// 6. Verify it should not be updated again before the rolling update ends
func Test_RU14_RollingUpdateWithPCSGScaleOutDuringUpdate(t *testing.T) {
	logger.Info("1. Initialize a 28-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:        28,
		ExpectedPods:       10,
		InitialPCSReplicas: 2,
		PostScalePods:      20,
	})
	defer cleanup()

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	tsLongerTimeout := *ts
	tsLongerTimeout.Timeout = 2 * time.Minute
	updateWait := tsLongerTimeout.triggerRollingUpdate(2, "pc-a", "pc-b", "pc-c")

	logger.Info("4. Scale out the PCSG during its rolling update (in parallel)")
	scaleWait := tsLongerTimeout.ScalePCSGAcrossAllReplicasAsync("workload1", "sg-x", 2, 3, 28, 0, 100) // 100ms delay so update is "first"

	tests.Logger.Info("5. Verify the scaled out replica is created with the correct specifications")
	// sg-x = 4 pods per replica (1 pc-b + 3 pc-c)
	// Scaling PCSG instances directly (workload1-0-sg-x, workload1-1-sg-x) since the PCS controller
	// only sets replicas during initial PCSG creation to support HPA scaling.
	// After scaling sg-x to 3 replicas: 2 PCS replicas x (2 pc-a + 3 sg-x x 4 pods) = 2 x 14 = 28 pods

	tests.Logger.Info("6. Verify it should not be updated again before the rolling update ends")
	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 28 {
		t.Fatalf("Expected 28 pods, got %d", len(pods.Items))
	}
	tracker.Stop()

	tests.Logger.Info("Rolling Update with PCSG scale-out during update test (RU-14) completed successfully!")
}
*/

/* This test is flaky. It sometimes fails with "rolling_updates_test.go:516: Expected 28 pods, got 30"
// Test_RU15_RollingUpdateWithPCSGScaleOutBeforeUpdate tests rolling update with scale-out on PCSG before it is updated
// Scenario RU-15:
// 1. Initialize a 28-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale out the PCSG before its rolling update starts
// 5. Verify the scaled out replica is created with the correct specifications
// 6. Verify it should not be updated again before the rolling update ends
func Test_RU15_RollingUpdateWithPCSGScaleOutBeforeUpdate(t *testing.T) {
	logger.Info("1. Initialize a 28-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:        28,
		ExpectedPods:       10,
		InitialPCSReplicas: 2,
		PostScalePods:      20,
	})
	defer cleanup()

	tests.Logger.Info("3. Scale out the PCSG before its rolling update starts (in parallel)")
	// Scaling PCSG instances directly (workload1-0-sg-x, workload1-1-sg-x) since the PCS controller
	// only sets replicas during initial PCSG creation to support HPA scaling.
	// After scaling sg-x to 3 replicas: 2 PCS replicas x (2 pc-a + 3 sg-x x 4 pods) = 2 x 14 = 28 pods
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 2 * time.Minute
	// Scale starts first (no delay)
	scaleWait := tsLongTimeout.ScalePCSGAcrossAllReplicasAsync("workload1", "sg-x", 2, 3, 28, 0, 0)

	tests.Logger.Info("4. Change the specification of pc-a, pc-b and pc-c")
	// Small delay so scale is clearly "first", then trigger update
	time.Sleep(100 * time.Millisecond)
	updateWait := tsLongTimeout.triggerRollingUpdate(2, "pc-a", "pc-b", "pc-c")

	tests.Logger.Info("5. Verify the scaled out replica is created with the correct specifications")
	tests.Logger.Info("6. Verify it should not be updated again before the rolling update ends")

	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 28 {
		t.Fatalf("Expected 28 pods, got %d", len(pods.Items))
	}
	tracker.Stop()

	tests.Logger.Info("Rolling Update with PCSG scale-out before update test (RU-15) completed successfully!")
}
*/

// Test_RU16_RollingUpdateWithPCSGScaleInDuringUpdate tests rolling update with scale-in on PCSG being updated
// Scenario RU-16:
// 1. Initialize a 28-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Scale up sg-x to 3 replicas (28 pods) so we can later scale in without going below minAvailable
// 4. Change the specification of pc-a, pc-b and pc-c
// 5. Scale in the PCSG during its rolling update (back to 2 replicas)
// 6. Verify the update goes through successfully
func Test_RU16_RollingUpdateWithPCSGScaleInDuringUpdate(t *testing.T) {
	logger.Info("1. Initialize a 28-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	logger.Info("3. Scale up sg-x to 3 replicas (28 pods) so we can later scale in")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:         28,
		ExpectedPods:        10,
		InitialPCSReplicas:  2,
		PostScalePods:       20,
		InitialPCSGReplicas: 3,
		PCSGName:            "sg-x",
		PostPCSGScalePods:   28,
	})
	defer cleanup()

	logger.Info("4. Change the specification of pc-a, pc-b and pc-c")
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 2 * time.Minute
	updateWait := tsLongTimeout.triggerRollingUpdate(2, "pc-a", "pc-b", "pc-c")

	tests.Logger.Info("5. Scale in the PCSG during its rolling update (in parallel)")
	// Scaling PCSG instances directly (workload1-0-sg-x, workload1-1-sg-x) since the PCS controller
	// only sets replicas during initial PCSG creation to support HPA scaling.
	// After scaling sg-x back to 2 replicas: 2 PCS replicas x (2 pc-a + 2 sg-x x 4 pods) = 2 x 10 = 20 pods
	scaleWait := tsLongTimeout.ScalePCSGAcrossAllReplicasAsync("workload1", "sg-x", 2, 2, 20, 0, 100) // 100ms delay so update is "first"

	tests.Logger.Info("6. Verify the update goes through successfully")

	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	// After scaling sg-x back to 2 replicas via PCS template (affects all PCS replicas):
	// 2 PCS replicas x (2 pc-a + 2 sg-x x 4 pods) = 2 x 10 = 20 pods
	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 20 {
		t.Fatalf("Expected 20 pods, got %d", len(pods.Items))
	}
	tracker.stop()

	tests.Logger.Info("Rolling Update with PCSG scale-in during update test (RU-16) completed successfully!")
}

/* This test is flaky. It sometimes fails with "Failed to wait for rolling update to complete: condition not met..."
// Test_RU17_RollingUpdateWithPCSGScaleInBeforeUpdate tests rolling update with scale-in on PCSG before it is updated
// Scenario RU-17:
// 1. Initialize a 28-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Scale up sg-x to 3 replicas (28 pods) so we can later scale in without going below minAvailable
// 4. Scale in the PCSG before its rolling update starts (back to 2 replicas)
// 5. Change the specification of pc-a, pc-b and pc-c
// 6. Verify the update goes through successfully
func Test_RU17_RollingUpdateWithPCSGScaleInBeforeUpdate(t *testing.T) {
	logger.Info("1. Initialize a 28-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	logger.Info("3. Scale up sg-x to 3 replicas (28 pods) so we can later scale in")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:         28,
		ExpectedPods:        10,
		InitialPCSReplicas:  2,
		PostScalePods:       20,
		InitialPCSGReplicas: 3,
		PCSGName:            "sg-x",
		PostPCSGScalePods:   28,
	})
	defer cleanup()

	tests.Logger.Info("4. Scale in the PCSG before its rolling update starts (in parallel)")
	// Scale starts first (no delay)
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 2 * time.Minute
	scaleWait := tsLongTimeout.ScalePCSGAcrossAllReplicasAsync("workload1", "sg-x", 2, 2, 20, 0, 0)

	tests.Logger.Info("5. Change the specification of pc-a, pc-b and pc-c")
	// Scaling PCSG instances directly (workload1-0-sg-x, workload1-1-sg-x) since the PCS controller
	// only sets replicas during initial PCSG creation to support HPA scaling.
	// After scaling sg-x back to 2 replicas: 2 PCS replicas x (2 pc-a + 2 sg-x x 4 pods) = 2 x 10 = 20 pods

	tests.Logger.Info("6. Verify the update goes through successfully")

	// Small delay so scale is clearly "first", then trigger update
	time.Sleep(100 * time.Millisecond)
	updateWait := tsLongTimeout.triggerRollingUpdate(2, "pc-a", "pc-b", "pc-c")

	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	// After scaling sg-x back to 2 replicas via PCS template (affects all PCS replicas):
	// 2 PCS replicas x (2 pc-a + 2 sg-x x 4 pods) = 2 x 10 = 20 pods
	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 20 {
		t.Fatalf("Expected 20 pods, got %d", len(pods.Items))
	}
	tracker.Stop()

	tests.Logger.Info("Rolling Update with PCSG scale-in before update test (RU-17) completed successfully!")
}
*/

// Test_RU18_RollingUpdateWithPodCliqueScaleOutDuringUpdate tests rolling update with scale-out on standalone PCLQ being updated
// Scenario RU-18:
// 1. Initialize a 24-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Change the specification of pc-a, pc-b and pc-c
// 4. Scale out the standalone PCLQ (pc-a) during its rolling update
// 5. Verify the scaled pods are created with the correct specifications
// 6. Verify they should not be updated again before the rolling update ends
func Test_RU18_RollingUpdateWithPodCliqueScaleOutDuringUpdate(t *testing.T) {
	ctx := context.Background()

	logger.Info("1. Initialize a 24-node Grove cluster")
	ts, cleanup := prepareTest(ctx, t, 24,
		WithWorkload(&WorkloadConfig{
			Name:         "workload1",
			YAMLPath:     "../../yaml/workload1.yaml",
			Namespace:    "default",
			ExpectedPods: 10,
		}),
	)
	defer cleanup()

	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	_, err := ts.DeployAndVerifyWorkload()
	if err != nil {
		t.Fatalf("Failed to deploy workload: %v", err)
	}

	ts.ScalePCSAndWait("workload1", 2, 20, 0)

	if err := ts.WaitForPods(20); err != nil {
		t.Fatalf("Failed to wait for pods to be ready: %v", err)
	}

	tracker := newRollingUpdateTracker()
	if err := tracker.Start(ts); err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.stop()

	logger.Info("3. Change the specification of pc-a, pc-b and pc-c")
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 4 * time.Minute // Extra headroom for rolling update + scale

	updateWait := tsLongTimeout.triggerRollingUpdate(2, "pc-a", "pc-b", "pc-c")

	tests.Logger.Info("4. Scale out the standalone PCLQ (pc-a) during its rolling update (in parallel)")

	logger.Info("5. Verify the scaled pods are created with the correct specifications")
	logger.Info("6. Verify they should not be updated again before the rolling update ends")
	scaleWait := tsLongTimeout.scalePodClique("pc-a", 4, 24, 100) // 100ms delay so update is "first"

	if err := <-updateWait; err != nil {
		// Diagnostics will be collected automatically by cleanup on test failure
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	tracker.stop()

	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) != 24 {
		t.Fatalf("Expected 24 pods after PodClique scale-out (4 pc-a + 4 pc-b + 12 pc-c), got %d", len(pods.Items))
	}

	tests.Logger.Info("Rolling Update with PodClique scale-out during update test (RU-18) completed successfully!")
}

// Test_RU19_RollingUpdateWithPodCliqueScaleOutBeforeUpdate tests rolling update with scale-out on standalone PCLQ before it is updated
// Scenario RU-19:
// 1. Initialize a 24-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Scale out the standalone PCLQ (pc-a) before its rolling update
// 4. Change the specification of pc-a, pc-b and pc-c
// 5. Verify the scaled pods are created with the correct specifications
// 6. Verify they should not be updated again before the rolling update ends
func Test_RU19_RollingUpdateWithPodCliqueScaleOutBeforeUpdate(t *testing.T) {
	logger.Info("1. Initialize a 24-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:        24,
		ExpectedPods:       10,
		InitialPCSReplicas: 2,
		PostScalePods:      20,
	})
	defer cleanup()

	tests.Logger.Info("3. Scale out the standalone PCLQ (pc-a) before its rolling update (in parallel)")
	// Scale starts first (no delay)
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 4 * time.Minute // Extra headroom for rolling update + scale
	scaleWait := tsLongTimeout.scalePodClique("pc-a", 4, 24, 0)

	tests.Logger.Info("4. Change the specification of pc-a, pc-b and pc-c")
	// Small delay so scale is clearly "first", then trigger update
	time.Sleep(100 * time.Millisecond)
	updateWait := tsLongTimeout.triggerRollingUpdate(2, "pc-a", "pc-b", "pc-c")

	tests.Logger.Info("5. Verify the scaled pods are created with the correct specifications")
	tests.Logger.Info("6. Verify they should not be updated again before the rolling update ends")

	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 24 {
		t.Fatalf("Expected 24 pods, got %d", len(pods.Items))
	}
	tracker.stop()

	tests.Logger.Info("Rolling Update with PodClique scale-out before update test (RU-19) completed successfully!")
}

// Test_RU20_RollingUpdateWithPodCliqueScaleInDuringUpdate tests rolling update with scale-in on standalone PCLQ being updated
// Scenario RU-20:
// 1. Initialize a 22-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Scale out pc-a to 3 replicas (above minAvailable=2) to allow scale-in during update
// 4. Change the specification of pc-a, pc-b and pc-c
// 5. Scale in the standalone PCLQ (pc-a) from 3 to 2 during its rolling update
// 6. Verify the update goes through successfully
func Test_RU20_RollingUpdateWithPodCliqueScaleInDuringUpdate(t *testing.T) {
	logger.Info("1. Initialize a 22-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:        22,
		ExpectedPods:       10,
		InitialPCSReplicas: 2,
		PostScalePods:      20,
	})
	defer cleanup()

	tests.Logger.Info("3. Scale out pc-a to 3 replicas (above minAvailable=2) to allow scale-in during update")
	// pc-a has minAvailable=2, so we scale up to 3 first to allow scale-in back to 2 during rolling update
	// Each PCS replica has: 2 pc-a + 8 sg-x pods = 10 pods
	// After scaling pc-a to 3: 2 PCS replicas × (3 pc-a + 8 sg-x) = 2 × 11 = 22 pods
	if err := ts.scalePodCliqueInPCS("pc-a", 3); err != nil {
		t.Fatalf("Failed to scale out PodClique pc-a: %v", err)
	}

	if err := ts.WaitForPods(22); err != nil {
		t.Fatalf("Failed to wait for pods after pc-a scale-out: %v", err)
	}

	tests.Logger.Info("4. Change the specification of pc-a, pc-b and pc-c")
	// Scale in from 3 to 2 (stays at minAvailable=2)
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 4 * time.Minute // Extra headroom for rolling update + scale

	updateWait := tsLongTimeout.triggerRollingUpdate(2, "pc-a", "pc-b", "pc-c")

	logger.Info("5. Scale in the standalone PCLQ (pc-a) from 3 to 2 during its rolling update (in parallel)")
	scaleWait := tsLongTimeout.scalePodClique("pc-a", 2, 20, 100) // 100ms delay so update is "first"

	tests.Logger.Info("6. Verify the update goes through successfully")
	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	// After scale-in from 3 to 2 pc-a: 2 PCS replicas × (2 pc-a + 8 sg-x) = 2 × 10 = 20 pods
	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 20 {
		t.Fatalf("Expected 20 pods, got %d", len(pods.Items))
	}
	tracker.stop()

	tests.Logger.Info("Rolling Update with PodClique scale-in during update test (RU-20) completed successfully!")
}

/* This test is flaky. It sometimes fails with "Failed to wait for rolling update to complete: condition not met..."
// Test_RU21_RollingUpdateWithPodCliqueScaleInBeforeUpdate tests rolling update with scale-in on standalone PCLQ before it is updated
// Scenario RU-21:
// 1. Initialize a 22-node Grove cluster
// 2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods
// 3. Scale out pc-a to 3 replicas (above minAvailable=2) to allow scale-in
// 4. Scale in pc-a from 3 to 2 before its rolling update
// 5. Change the specification of pc-a, pc-b and pc-c
// 6. Verify the update goes through successfully
func Test_RU21_RollingUpdateWithPodCliqueScaleInBeforeUpdate(t *testing.T) {
	logger.Info("1. Initialize a 22-node Grove cluster")
	logger.Info("2. Deploy workload WL1 with 2 replicas, and verify 20 newly created pods")
	ts, cleanup, tracker := setupRollingUpdateTest(t, RollingUpdateTestConfig{
		WorkerNodes:        22,
		ExpectedPods:       10,
		InitialPCSReplicas: 2,
		PostScalePods:      20,
	})
	defer cleanup()

	tests.Logger.Info("3. Scale out pc-a to 3 replicas (above minAvailable=2) to allow scale-in")
	// pc-a has minAvailable=2, so we scale up to 3 first to allow scale-in back to 2
	// After scaling pc-a to 3: 2 PCS replicas × (3 pc-a + 8 sg-x) = 2 × 11 = 22 pods
	if err := ts.scalePodCliqueInPCS("pc-a", 3); err != nil {
		t.Fatalf("Failed to scale out PodClique pc-a: %v", err)
	}

	if err := ts.WaitForPods(22); err != nil {
		t.Fatalf("Failed to wait for pods after pc-a scale-out: %v", err)
	}

	logger.Info("4. Scale in pc-a from 3 to 2 before its rolling update (in parallel)")
	tsLongTimeout := *ts
	tsLongTimeout.Timeout = 4 * time.Minute // Extra headroom for rolling update + scale
	// Scale starts first (no delay) - scaling in from 3 to 2 (stays at minAvailable=2)
	scaleWait := tsLongTimeout.scalePodClique("pc-a", 2, 20, 0)

	tests.Logger.Info("5. Change the specification of pc-a, pc-b and pc-c")
	// Small delay so scale is clearly "first", then trigger update
	time.Sleep(100 * time.Millisecond)
	updateWait := tsLongTimeout.triggerRollingUpdate(2, "pc-a", "pc-b", "pc-c")

	tests.Logger.Info("6. Verify the update goes through successfully")

	if err := <-updateWait; err != nil {
		t.Fatalf("Rolling update failed: %v", err)
	}
	if err := <-scaleWait; err != nil {
		t.Fatalf("Scale operation failed: %v", err)
	}

	// After scale-in from 3 to 2 pc-a: 2 PCS replicas × (2 pc-a + 8 sg-x) = 2 × 10 = 20 pods
	pods, err := ts.ListPods()
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 20 {
		t.Fatalf("Expected 20 pods, got %d", len(pods.Items))
	}
	tracker.Stop()

	tests.Logger.Info("Rolling Update with PodClique scale-in before update test (RU-21) completed successfully!")
}
*/
