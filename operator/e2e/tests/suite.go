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
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ai-dynamo/grove/operator/e2e/diagnostics"
	"github.com/ai-dynamo/grove/operator/e2e/grove"
	"github.com/ai-dynamo/grove/operator/e2e/k8s"
	"github.com/ai-dynamo/grove/operator/e2e/setup"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestContext is the primary per-test helper struct that composes domain-specific managers.
// Clients are created once and shared across all managers.
type TestContext struct {
	T   *testing.T
	Ctx context.Context

	// Shared clients (created once per test run, goroutine-safe)
	Clients *k8s.Clients

	// Domain managers (created per-suite, hold reference to shared Clients)
	Pods      *k8s.PodManager
	Nodes     *k8s.NodeManager
	Resources *k8s.ResourceManager
	Workloads *grove.WorkloadManager
	Topology  *grove.TopologyVerifier
	PodGroups *grove.PodGroupVerifier
	Config    *grove.OperatorConfig
	Diag      *diagnostics.DiagCollector

	// Per-suite configuration
	Namespace string
	Timeout   time.Duration
	Interval  time.Duration
	Workload  *WorkloadConfig
	NodeNames []string // Reserved nodes for this suite (future: from NodePool)
}

// TestOption configures a TestContext.
type TestOption func(*TestContext)

// WithNamespace sets the namespace for the test context.
func WithNamespace(ns string) TestOption {
	return func(ts *TestContext) { ts.Namespace = ns }
}

// WithTimeout sets the default poll timeout.
func WithTimeout(d time.Duration) TestOption {
	return func(ts *TestContext) { ts.Timeout = d }
}

// WithInterval sets the default poll interval.
func WithInterval(d time.Duration) TestOption {
	return func(ts *TestContext) { ts.Interval = d }
}

// WithWorkload sets the workload configuration.
func WithWorkload(wc *WorkloadConfig) TestOption {
	return func(ts *TestContext) { ts.Workload = wc }
}

// NewTestContext creates a TestContext from shared clients with optional configuration.
func NewTestContext(t *testing.T, ctx context.Context, clients *k8s.Clients, opts ...TestOption) *TestContext {
	ts := &TestContext{
		T:         t,
		Ctx:       ctx,
		Clients:   clients,
		Namespace: "default",
		Timeout:   defaultPollTimeout,
		Interval:  defaultPollInterval,
	}

	for _, opt := range opts {
		opt(ts)
	}

	// Initialize managers
	ts.Pods = k8s.NewPodManager(clients, logger)
	ts.Nodes = k8s.NewNodeManager(clients, logger)
	ts.Resources = k8s.NewResourceManager(clients, logger)
	ts.Workloads = grove.NewWorkloadManager(clients, ts.Resources, ts.Pods, logger)
	ts.Topology = grove.NewTopologyVerifier(clients, logger)
	ts.PodGroups = grove.NewPodGroupVerifier(clients, logger)
	ts.Config = grove.NewOperatorConfig(clients)

	// Initialize diagnostics
	diagMode := os.Getenv(diagnostics.ModeEnvVar)
	if diagMode == "" {
		diagMode = diagnostics.ModeFile
	}
	diagDir := os.Getenv(diagnostics.DirEnvVar)
	ts.Diag = diagnostics.NewDiagCollector(clients, ts.Namespace, diagMode, diagDir, logger)

	return ts
}

// prepareTest prepares the shared cluster and returns a TestContext with cleanup function.
func prepareTest(ctx context.Context, t *testing.T, requiredWorkerNodes int, opts ...TestOption) (*TestContext, func()) {
	t.Helper()

	sharedCluster := setup.SharedCluster(logger)
	if err := sharedCluster.PrepareForTest(ctx, requiredWorkerNodes); err != nil {
		t.Fatalf("Failed to prepare shared cluster: %v", err)
	}

	clients := sharedCluster.GetAllClients()
	ts := NewTestContext(t, ctx, clients, opts...)

	cleanup := func() {
		if t.Failed() {
			ts.Diag.CollectAll(t.Name())
		}

		if err := sharedCluster.CleanupWorkloads(ctx); err != nil {
			logger.Error("================================================================================")
			logger.Error("=== CLEANUP FAILURE - COLLECTING DIAGNOSTICS ===")
			logger.Error("================================================================================")
			ts.Diag.CollectAll(t.Name())
			sharedCluster.MarkCleanupFailed(err)
			t.Fatalf("Failed to cleanup workloads: %v. All subsequent tests will fail.", err)
		}
	}

	return ts, cleanup
}

// --- Convenience methods that delegate to managers with suite-scoped defaults ---

// getLabelSelector returns the label selector for the current workload.
func (ts *TestContext) getLabelSelector() string {
	if ts.Workload == nil {
		return ""
	}
	return ts.Workload.GetLabelSelector()
}

// PollForCondition wraps k8s.PollForCondition with suite defaults.
func (ts *TestContext) PollForCondition(condition func() (bool, error)) error {
	return k8s.PollForCondition(ts.Ctx, ts.Timeout, ts.Interval, condition)
}

// ListPods lists pods matching the current workload's label selector.
func (ts *TestContext) ListPods() (*v1.PodList, error) {
	return ts.Pods.List(ts.Ctx, ts.Namespace, ts.getLabelSelector())
}

// WaitForPods waits for the expected pod count to be ready.
func (ts *TestContext) WaitForPods(expectedCount int) error {
	return ts.Pods.WaitForReady(ts.Ctx, []string{ts.Namespace}, ts.getLabelSelector(), expectedCount, ts.Timeout, ts.Interval)
}

// WaitForPodCount waits for a specific number of pods and returns them.
func (ts *TestContext) WaitForPodCount(expectedCount int) (*v1.PodList, error) {
	return ts.Pods.WaitForCount(ts.Ctx, ts.Namespace, ts.getLabelSelector(), expectedCount, ts.Timeout, ts.Interval)
}

// WaitForPodCountAndPhases waits for pods to reach specific total count and phase counts.
func (ts *TestContext) WaitForPodCountAndPhases(expectedTotal, expectedRunning, expectedPending int) error {
	return ts.Pods.WaitForCountAndPhases(ts.Ctx, ts.Namespace, ts.getLabelSelector(), expectedTotal, expectedRunning, expectedPending, ts.Timeout, ts.Interval)
}

// WaitForPodPhases waits for pods to reach specific running and pending counts.
func (ts *TestContext) WaitForPodPhases(expectedRunning, expectedPending int) error {
	return ts.PollForCondition(func() (bool, error) {
		pods, err := ts.ListPods()
		if err != nil {
			return false, err
		}
		count := k8s.CountPodsByPhase(pods)
		return count.Running == expectedRunning && count.Pending == expectedPending, nil
	})
}

// WaitForReadyPods waits for a specific number of pods to be ready.
func (ts *TestContext) WaitForReadyPods(expectedReady int) error {
	ts.T.Helper()
	return ts.PollForCondition(func() (bool, error) {
		pods, err := ts.ListPods()
		if err != nil {
			return false, err
		}
		return k8s.CountReady(pods) == expectedReady, nil
	})
}

// WaitForRunningPods waits for a specific number of pods to be in Running phase.
func (ts *TestContext) WaitForRunningPods(expectedRunning int) error {
	ts.T.Helper()
	return ts.PollForCondition(func() (bool, error) {
		pods, err := ts.ListPods()
		if err != nil {
			return false, err
		}
		count := k8s.CountPodsByPhase(pods)
		return count.Running == expectedRunning, nil
	})
}

// CordonNode marks a node as unschedulable.
func (ts *TestContext) CordonNode(nodeName string) error {
	return ts.Nodes.Cordon(ts.Ctx, nodeName)
}

// UncordonNode marks a node as schedulable.
func (ts *TestContext) UncordonNode(nodeName string) error {
	return ts.Nodes.Uncordon(ts.Ctx, nodeName)
}

// CordonNodes cordons multiple nodes.
func (ts *TestContext) CordonNodes(nodes []string) {
	ts.T.Helper()
	for _, nodeName := range nodes {
		if err := ts.CordonNode(nodeName); err != nil {
			ts.T.Fatalf("Failed to cordon node %s: %v", nodeName, err)
		}
	}
}

// UncordonNodes uncordons multiple nodes.
func (ts *TestContext) UncordonNodes(nodes []string) {
	ts.T.Helper()
	for _, nodeName := range nodes {
		if err := ts.UncordonNode(nodeName); err != nil {
			ts.T.Fatalf("Failed to uncordon node %s: %v", nodeName, err)
		}
	}
}

// GetWorkerNodes retrieves the names of all worker nodes in the cluster.
func (ts *TestContext) GetWorkerNodes() ([]string, error) {
	return ts.Nodes.GetWorkerNodes(ts.Ctx)
}

// ScalePCS scales a PodCliqueSet to the specified replica count.
func (ts *TestContext) ScalePCS(name string, replicas int) error {
	return ts.Workloads.ScalePCS(ts.Ctx, ts.Namespace, name, replicas)
}

// ScalePCSG scales a PodCliqueScalingGroup to the specified replica count.
func (ts *TestContext) ScalePCSG(name string, replicas int) error {
	return ts.Workloads.ScalePCSG(ts.Ctx, ts.Namespace, name, replicas, ts.Timeout, ts.Interval)
}

// ApplyYAMLFile applies a YAML file to the cluster.
func (ts *TestContext) ApplyYAMLFile(yamlPath string) ([]k8s.AppliedResource, error) {
	return ts.Resources.ApplyYAMLFile(ts.Ctx, yamlPath, ts.Namespace)
}

// DeployAndVerifyWorkload applies a workload YAML and waits for the expected pod count.
func (ts *TestContext) DeployAndVerifyWorkload() (*v1.PodList, error) {
	ts.T.Helper()
	if ts.Workload == nil {
		return nil, fmt.Errorf("ts.Workload is nil, must be set before calling DeployAndVerifyWorkload")
	}

	_, err := ts.ApplyYAMLFile(ts.Workload.YAMLPath)
	if err != nil {
		return nil, fmt.Errorf("failed to apply workload YAML: %w", err)
	}

	pods, err := ts.WaitForPodCount(ts.Workload.ExpectedPods)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for pods to be created: %w", err)
	}

	return pods, nil
}

// VerifyAllPodsArePending verifies that all pods matching the label selector are pending.
func (ts *TestContext) VerifyAllPodsArePending() error {
	return ts.PollForCondition(func() (bool, error) {
		pods, err := ts.ListPods()
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != v1.PodPending {
				return false, nil
			}
		}
		return true, nil
	})
}

// VerifyPodsArePendingWithUnschedulableEvents verifies that pods are pending with Unschedulable events.
func (ts *TestContext) VerifyPodsArePendingWithUnschedulableEvents(allPodsMustBePending bool, expectedPendingCount int) error {
	if allPodsMustBePending {
		if err := ts.VerifyAllPodsArePending(); err != nil {
			return fmt.Errorf("not all pods are pending: %w", err)
		}
	}

	return ts.PollForCondition(func() (bool, error) {
		pods, err := ts.ListPods()
		if err != nil {
			return false, err
		}

		podsWithUnschedulableEvent := 0
		pendingCount := 0

		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodPending {
				pendingCount++

				events, err := ts.Clients.Clientset.CoreV1().Events(ts.Namespace).List(ts.Ctx, metav1.ListOptions{
					FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Pod", pod.Name),
				})
				if err != nil {
					return false, err
				}

				var mostRecentEvent *v1.Event
				for i := range events.Items {
					event := &events.Items[i]
					if mostRecentEvent == nil || event.LastTimestamp.After(mostRecentEvent.LastTimestamp.Time) {
						mostRecentEvent = event
					}
				}

				if mostRecentEvent != nil &&
					mostRecentEvent.Type == v1.EventTypeWarning &&
					((mostRecentEvent.Reason == "Unschedulable" && mostRecentEvent.Source.Component == "kai-scheduler") ||
						(mostRecentEvent.Reason == "PodGrouperWarning" && mostRecentEvent.Source.Component == "pod-grouper")) {
					podsWithUnschedulableEvent++
				}
			}
		}

		if expectedPendingCount > 0 && pendingCount != expectedPendingCount {
			return false, nil
		}

		return podsWithUnschedulableEvent == pendingCount, nil
	})
}

// ListPodsAndAssertDistinctNodes lists pods and asserts they are on distinct nodes.
func (ts *TestContext) ListPodsAndAssertDistinctNodes() {
	ts.T.Helper()
	pods, err := ts.ListPods()
	if err != nil {
		ts.T.Fatalf("Failed to list workload pods: %v", err)
	}
	assertPodsOnDistinctNodes(ts.T, pods.Items)
}

// SetupAndCordonNodes retrieves worker nodes and cordons the specified number.
func (ts *TestContext) SetupAndCordonNodes(numToCordon int) []string {
	ts.T.Helper()

	workerNodes, err := ts.GetWorkerNodes()
	if err != nil {
		ts.T.Fatalf("Failed to get worker nodes: %v", err)
	}

	if len(workerNodes) < numToCordon {
		ts.T.Fatalf("expected at least %d worker nodes to cordon, but found %d", numToCordon, len(workerNodes))
	}

	nodesToCordon := workerNodes[:numToCordon]
	ts.CordonNodes(nodesToCordon)

	return nodesToCordon
}

// UncordonNodesAndWaitForPods uncordons nodes and waits for pods to be ready.
func (ts *TestContext) UncordonNodesAndWaitForPods(nodes []string, expectedPods int) {
	ts.T.Helper()
	ts.UncordonNodes(nodes)
	if err := ts.WaitForPods(expectedPods); err != nil {
		ts.T.Fatalf("Failed to wait for pods to be ready: %v", err)
	}
}

// VerifyAllPodsArePendingWithSleep verifies all pods are pending after a fixed delay.
func (ts *TestContext) VerifyAllPodsArePendingWithSleep() {
	ts.T.Helper()
	time.Sleep(30 * time.Second)
	if err := ts.VerifyAllPodsArePending(); err != nil {
		ts.T.Fatalf("Failed to verify all pods are pending: %v", err)
	}
}

// WaitForPodConditions polls until the expected pod state is reached.
func (ts *TestContext) WaitForPodConditions(expectedTotalPods, expectedPending int) (int, int, int, error) {
	var lastTotal, lastRunning, lastPending int

	err := ts.PollForCondition(func() (bool, error) {
		pods, err := ts.ListPods()
		if err != nil {
			return false, err
		}
		count := k8s.CountPodsByPhase(pods)
		lastTotal = count.Total
		lastRunning = count.Running
		lastPending = count.Pending
		return lastTotal == expectedTotalPods && lastPending == expectedPending, nil
	})

	return lastTotal, lastRunning, lastPending, err
}

// ScalePCSAndWait scales a PCS and waits for the expected pod conditions.
func (ts *TestContext) ScalePCSAndWait(pcsName string, replicas int32, expectedTotalPods, expectedPending int) {
	ts.T.Helper()

	if err := ts.ScalePCS(pcsName, int(replicas)); err != nil {
		ts.T.Fatalf("Failed to scale PodCliqueSet %s: %v", pcsName, err)
	}

	totalPods, runningPods, pendingPods, err := ts.WaitForPodConditions(expectedTotalPods, expectedPending)
	if err != nil {
		ts.T.Fatalf("Failed to wait for expected pod conditions after PCS scaling: %v. Final state: total=%d, running=%d, pending=%d (expected: total=%d, pending=%d)",
			err, totalPods, runningPods, pendingPods, expectedTotalPods, expectedPending)
	}
}

// ScalePCSGInstanceAndWait scales a specific PCSG instance and waits for expected pod conditions.
func (ts *TestContext) ScalePCSGInstanceAndWait(pcsgInstanceName string, replicas int32, expectedTotalPods, expectedPending int) {
	ts.T.Helper()

	if err := ts.ScalePCSG(pcsgInstanceName, int(replicas)); err != nil {
		ts.T.Fatalf("Failed to scale PodCliqueScalingGroup instance %s: %v", pcsgInstanceName, err)
	}

	totalPods, runningPods, pendingPods, err := ts.WaitForPodConditions(expectedTotalPods, expectedPending)
	if err != nil {
		ts.T.Fatalf("Failed to wait for expected pod conditions after PCSG instance scaling: %v. Final state: total=%d, running=%d, pending=%d (expected: total=%d, pending=%d)",
			err, totalPods, runningPods, pendingPods, expectedTotalPods, expectedPending)
	}
}

// ScalePCSGAcrossAllReplicasAndWait scales a PCSG across all PCS replicas and waits.
func (ts *TestContext) ScalePCSGAcrossAllReplicasAndWait(pcsName, pcsgName string, pcsReplicas, pcsgReplicas int32, expectedTotalPods, expectedPending int) {
	ts.T.Helper()

	for replicaIndex := int32(0); replicaIndex < pcsReplicas; replicaIndex++ {
		pcsgInstanceName := fmt.Sprintf("%s-%d-%s", pcsName, replicaIndex, pcsgName)
		if err := ts.ScalePCSG(pcsgInstanceName, int(pcsgReplicas)); err != nil {
			ts.T.Fatalf("Failed to scale PodCliqueScalingGroup instance %s: %v", pcsgInstanceName, err)
		}
	}

	totalPods, runningPods, pendingPods, err := ts.WaitForPodConditions(expectedTotalPods, expectedPending)
	if err != nil {
		ts.T.Fatalf("Failed to wait for expected pod conditions after PCSG scaling across all replicas: %v. Final state: total=%d, running=%d, pending=%d (expected: total=%d, pending=%d)",
			err, totalPods, runningPods, pendingPods, expectedTotalPods, expectedPending)
	}
}

// ScalePCSAsync scales a PCS asynchronously and returns an error channel.
func (ts *TestContext) ScalePCSAsync(pcsName string, replicas int32, expectedTotalPods, expectedPending, delayMs int) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		startTime := time.Now()

		if delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}

		if err := ts.ScalePCS(pcsName, int(replicas)); err != nil {
			errCh <- fmt.Errorf("failed to scale PodCliqueSet %s: %w", pcsName, err)
			return
		}

		totalPods, runningPods, pendingPods, err := ts.WaitForPodConditions(expectedTotalPods, expectedPending)
		elapsed := time.Since(startTime)
		if err != nil {
			logger.Infof("[scalePCS] Scale %s FAILED after %v: total=%d, running=%d, pending=%d (expected: total=%d, pending=%d)",
				pcsName, elapsed, totalPods, runningPods, pendingPods, expectedTotalPods, expectedPending)
			errCh <- fmt.Errorf("failed to wait for expected pod conditions after PCS scaling: %w. Final state: total=%d, running=%d, pending=%d (expected: total=%d, pending=%d)",
				err, totalPods, runningPods, pendingPods, expectedTotalPods, expectedPending)
			return
		}
		logger.Infof("[scalePCS] Scale %s completed in %v (replicas=%d, pods=%d)", pcsName, elapsed, replicas, totalPods)
		errCh <- nil
	}()
	return errCh
}

// ScalePCSGAcrossAllReplicasAsync scales a PCSG across all PCS replicas asynchronously.
func (ts *TestContext) ScalePCSGAcrossAllReplicasAsync(pcsName, pcsgName string, pcsReplicas, pcsgReplicas int32, expectedTotalPods, expectedPending, delayMs int) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		startTime := time.Now()

		if delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}

		for replicaIndex := int32(0); replicaIndex < pcsReplicas; replicaIndex++ {
			pcsgInstanceName := fmt.Sprintf("%s-%d-%s", pcsName, replicaIndex, pcsgName)
			if err := ts.ScalePCSG(pcsgInstanceName, int(pcsgReplicas)); err != nil {
				errCh <- fmt.Errorf("failed to scale PodCliqueScalingGroup instance %s: %w", pcsgInstanceName, err)
				return
			}
		}

		totalPods, runningPods, pendingPods, err := ts.WaitForPodConditions(expectedTotalPods, expectedPending)
		elapsed := time.Since(startTime)
		if err != nil {
			logger.Infof("[scalePCSGAcrossAllReplicas] Scale %s FAILED after %v: total=%d, running=%d, pending=%d (expected: total=%d, pending=%d)",
				pcsgName, elapsed, totalPods, runningPods, pendingPods, expectedTotalPods, expectedPending)
			errCh <- fmt.Errorf("failed to wait for expected pod conditions after PCSG scaling across all replicas: %w. Final state: total=%d, running=%d, pending=%d (expected: total=%d, pending=%d)",
				err, totalPods, runningPods, pendingPods, expectedTotalPods, expectedPending)
			return
		}
		logger.Infof("[scalePCSGAcrossAllReplicas] Scale %s completed in %v (pcsgReplicas=%d, pods=%d)", pcsgName, elapsed, pcsgReplicas, totalPods)
		errCh <- nil
	}()
	return errCh
}
