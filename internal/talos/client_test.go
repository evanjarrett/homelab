package talos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// ============================================================================
// MockClient Tests
// ============================================================================

func TestMockClient_DefaultBehavior(t *testing.T) {
	mock := &MockClient{}

	t.Run("Close returns nil", func(t *testing.T) {
		err := mock.Close()
		assert.NoError(t, err)
	})

	t.Run("GetVersion returns default", func(t *testing.T) {
		version, err := mock.GetVersion(context.Background(), "192.168.1.1")
		require.NoError(t, err)
		assert.Equal(t, "1.7.0", version)
	})

	t.Run("GetMachineType returns unknown", func(t *testing.T) {
		machineType, err := mock.GetMachineType(context.Background(), "192.168.1.1")
		require.NoError(t, err)
		assert.Equal(t, "unknown", machineType)
	})

	t.Run("IsReachable returns true", func(t *testing.T) {
		reachable := mock.IsReachable(context.Background(), "192.168.1.1")
		assert.True(t, reachable)
	})

	t.Run("Upgrade returns nil", func(t *testing.T) {
		err := mock.Upgrade(context.Background(), "192.168.1.1", "image:v1.7.0", true)
		assert.NoError(t, err)
	})

	t.Run("WaitForNode returns nil", func(t *testing.T) {
		err := mock.WaitForNode(context.Background(), "192.168.1.1", time.Minute)
		assert.NoError(t, err)
	})

	t.Run("GetNodeStatus returns populated status", func(t *testing.T) {
		status := mock.GetNodeStatus(context.Background(), "192.168.1.1", "profile-a", "worker", true)
		assert.Equal(t, "192.168.1.1", status.IP)
		assert.Equal(t, "profile-a", status.Profile)
		assert.Equal(t, "worker", status.Role)
		assert.Equal(t, "1.7.0", status.Version)
		assert.True(t, status.Secureboot)
		assert.True(t, status.Reachable)
	})

	t.Run("WatchUpgrade returns nil", func(t *testing.T) {
		err := mock.WatchUpgrade(context.Background(), "192.168.1.1", time.Minute, nil)
		assert.NoError(t, err)
	})

	t.Run("WaitForServices returns nil", func(t *testing.T) {
		err := mock.WaitForServices(context.Background(), "192.168.1.1", []string{"etcd", "kubelet"}, time.Minute)
		assert.NoError(t, err)
	})

	t.Run("WaitForStaticPods returns nil", func(t *testing.T) {
		err := mock.WaitForStaticPods(context.Background(), "192.168.1.1", time.Minute)
		assert.NoError(t, err)
	})
}

func TestMockClient_CustomFunctions(t *testing.T) {
	t.Run("custom GetVersion", func(t *testing.T) {
		mock := &MockClient{
			GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
				if nodeIP == "192.168.1.1" {
					return "1.8.0", nil
				}
				return "", errors.New("node not found")
			},
		}

		version, err := mock.GetVersion(context.Background(), "192.168.1.1")
		require.NoError(t, err)
		assert.Equal(t, "1.8.0", version)

		_, err = mock.GetVersion(context.Background(), "192.168.1.99")
		assert.Error(t, err)
	})

	t.Run("custom IsReachable", func(t *testing.T) {
		unreachableNodes := map[string]bool{"192.168.1.2": true}
		mock := &MockClient{
			IsReachableFunc: func(ctx context.Context, nodeIP string) bool {
				return !unreachableNodes[nodeIP]
			},
		}

		assert.True(t, mock.IsReachable(context.Background(), "192.168.1.1"))
		assert.False(t, mock.IsReachable(context.Background(), "192.168.1.2"))
	})

	t.Run("custom Upgrade with error", func(t *testing.T) {
		mock := &MockClient{
			UpgradeFunc: func(ctx context.Context, nodeIP, image string, preserve bool) error {
				return errors.New("upgrade failed")
			},
		}

		err := mock.Upgrade(context.Background(), "192.168.1.1", "image:v1.7.0", true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "upgrade failed")
	})

	t.Run("custom WaitForNode timeout", func(t *testing.T) {
		mock := &MockClient{
			WaitForNodeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration) error {
				return errors.New("timeout waiting for node")
			},
		}

		err := mock.WaitForNode(context.Background(), "192.168.1.1", time.Minute)
		assert.Error(t, err)
	})

	t.Run("custom GetNodeStatus unreachable", func(t *testing.T) {
		mock := &MockClient{
			GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) NodeStatus {
				return NodeStatus{
					IP:        nodeIP,
					Profile:   profile,
					Role:      role,
					Version:   "N/A",
					Reachable: false,
				}
			},
		}

		status := mock.GetNodeStatus(context.Background(), "192.168.1.1", "test", "worker", false)
		assert.False(t, status.Reachable)
		assert.Equal(t, "N/A", status.Version)
	})

	t.Run("custom WatchUpgrade with callback", func(t *testing.T) {
		var callbackCalls []UpgradeProgress
		mock := &MockClient{
			WatchUpgradeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration, onProgress ProgressCallback) error {
				// Simulate progress events
				if onProgress != nil {
					onProgress(UpgradeProgress{Stage: "upgrading", Phase: "prepare"})
					onProgress(UpgradeProgress{Stage: "rebooting"})
					onProgress(UpgradeProgress{Stage: "running", Done: true})
				}
				return nil
			},
		}

		err := mock.WatchUpgrade(context.Background(), "192.168.1.1", time.Minute, func(p UpgradeProgress) {
			callbackCalls = append(callbackCalls, p)
		})
		require.NoError(t, err)
		assert.Len(t, callbackCalls, 3)
		assert.Equal(t, "upgrading", callbackCalls[0].Stage)
		assert.Equal(t, "running", callbackCalls[2].Stage)
		assert.True(t, callbackCalls[2].Done)
	})

	t.Run("custom WaitForServices with error", func(t *testing.T) {
		mock := &MockClient{
			WaitForServicesFunc: func(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error {
				return errors.New("service not healthy")
			},
		}
		err := mock.WaitForServices(context.Background(), "192.168.1.1", []string{"etcd"}, time.Minute)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "service not healthy")
	})

	t.Run("custom WaitForStaticPods with error", func(t *testing.T) {
		mock := &MockClient{
			WaitForStaticPodsFunc: func(ctx context.Context, nodeIP string, timeout time.Duration) error {
				return errors.New("pods not ready")
			},
		}
		err := mock.WaitForStaticPods(context.Background(), "192.168.1.1", time.Minute)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pods not ready")
	})
}

// ============================================================================
// NodeStatus Tests
// ============================================================================

func TestNodeStatus_Struct(t *testing.T) {
	status := NodeStatus{
		IP:          "192.168.1.1",
		Profile:     "amd64-intel",
		Role:        "controlplane",
		Version:     "1.7.0",
		MachineType: "controlplane",
		Secureboot:  true,
		Reachable:   true,
	}

	assert.Equal(t, "192.168.1.1", status.IP)
	assert.Equal(t, "amd64-intel", status.Profile)
	assert.Equal(t, "controlplane", status.Role)
	assert.Equal(t, "1.7.0", status.Version)
	assert.Equal(t, "controlplane", status.MachineType)
	assert.True(t, status.Secureboot)
	assert.True(t, status.Reachable)
}

func TestNodeStatus_Unreachable(t *testing.T) {
	status := NodeStatus{
		IP:          "192.168.1.99",
		Profile:     "unknown",
		Role:        "worker",
		Version:     "N/A",
		MachineType: "unknown",
		Secureboot:  false,
		Reachable:   false,
	}

	assert.Equal(t, "192.168.1.99", status.IP)
	assert.Equal(t, "N/A", status.Version)
	assert.False(t, status.Reachable)
}

// ============================================================================
// UpgradeProgress Tests
// ============================================================================

func TestUpgradeProgress_Struct(t *testing.T) {
	progress := UpgradeProgress{
		Stage:  "upgrading",
		Phase:  "install",
		Task:   "downloading image",
		Action: "START",
		Done:   false,
	}

	assert.Equal(t, "upgrading", progress.Stage)
	assert.Equal(t, "install", progress.Phase)
	assert.Equal(t, "downloading image", progress.Task)
	assert.Equal(t, "START", progress.Action)
	assert.False(t, progress.Done)
}

func TestUpgradeProgress_Done(t *testing.T) {
	progress := UpgradeProgress{
		Stage: "running",
		Done:  true,
	}

	assert.True(t, progress.Done)
}

func TestUpgradeProgress_Error(t *testing.T) {
	progress := UpgradeProgress{
		Stage: "upgrading",
		Error: "failed to download image",
	}

	assert.NotEmpty(t, progress.Error)
}

// ============================================================================
// Interface Compliance Tests
// ============================================================================

func TestMockClient_ImplementsInterface(t *testing.T) {
	// This test verifies that MockClient implements TalosClientInterface
	var client TalosClientInterface = &MockClient{}
	assert.NotNil(t, client)
}

// ============================================================================
// Context Handling Tests
// ============================================================================

func TestMockClient_ContextCancellation(t *testing.T) {
	mock := &MockClient{
		WaitForNodeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(timeout):
				return nil
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := mock.WaitForNode(ctx, "192.168.1.1", time.Hour)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestMockClient_ContextTimeout(t *testing.T) {
	mock := &MockClient{
		WaitForNodeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second): // Simulates long operation
				return nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := mock.WaitForNode(ctx, "192.168.1.1", time.Hour)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

// ============================================================================
// Upgrade Workflow Simulation Tests
// ============================================================================

func TestMockClient_SimulateUpgradeWorkflow(t *testing.T) {
	// Track upgrade state
	upgradeStarted := false
	nodeVersion := "1.6.0"

	mock := &MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return nodeVersion, nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, preserve bool) error {
			upgradeStarted = true
			return nil
		},
		WatchUpgradeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration, onProgress ProgressCallback) error {
			if !upgradeStarted {
				return errors.New("upgrade not started")
			}
			// Simulate upgrade completing
			nodeVersion = "1.7.0"
			if onProgress != nil {
				onProgress(UpgradeProgress{Stage: "running", Done: true})
			}
			return nil
		},
	}

	ctx := context.Background()

	// Check initial version
	version, err := mock.GetVersion(ctx, "192.168.1.1")
	require.NoError(t, err)
	assert.Equal(t, "1.6.0", version)

	// Start upgrade
	err = mock.Upgrade(ctx, "192.168.1.1", "image:v1.7.0", true)
	require.NoError(t, err)
	assert.True(t, upgradeStarted)

	// Watch for completion
	var finalProgress UpgradeProgress
	err = mock.WatchUpgrade(ctx, "192.168.1.1", time.Minute, func(p UpgradeProgress) {
		finalProgress = p
	})
	require.NoError(t, err)
	assert.True(t, finalProgress.Done)

	// Check new version
	version, err = mock.GetVersion(ctx, "192.168.1.1")
	require.NoError(t, err)
	assert.Equal(t, "1.7.0", version)
}

// ============================================================================
// Service List Tests
// ============================================================================

func TestGetControlPlaneServices(t *testing.T) {
	services := GetControlPlaneServices()
	expected := []string{"etcd", "kubelet", "apid", "trustd"}
	assert.Equal(t, expected, services)
	assert.Len(t, services, 4)
}

func TestGetWorkerServices(t *testing.T) {
	services := GetWorkerServices()
	expected := []string{"kubelet", "apid", "trustd"}
	assert.Equal(t, expected, services)
	assert.Len(t, services, 3)
}

// ============================================================================
// parseEvent Tests
// ============================================================================

func TestParseEvent_SequenceEvent(t *testing.T) {
	c := &Client{} // nil talos/k8s clients are fine - parseEvent doesn't use them

	t.Run("sequence start", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.SequenceEvent{
				Sequence: "upgrade",
				Action:   machine.SequenceEvent_START,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "upgrade", progress.Phase)
		assert.Equal(t, "START", progress.Action)
		assert.Empty(t, progress.Error)
	})

	t.Run("sequence stop", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.SequenceEvent{
				Sequence: "reboot",
				Action:   machine.SequenceEvent_STOP,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "reboot", progress.Phase)
		assert.Equal(t, "STOP", progress.Action)
	})

	t.Run("sequence with error", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.SequenceEvent{
				Sequence: "upgrade",
				Action:   machine.SequenceEvent_START,
				Error: &common.Error{
					Message: "upgrade failed: disk full",
				},
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "upgrade failed: disk full", progress.Error)
	})
}

func TestParseEvent_PhaseEvent(t *testing.T) {
	c := &Client{}

	t.Run("phase start", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.PhaseEvent{
				Phase:  "install",
				Action: machine.PhaseEvent_START,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "install", progress.Phase)
		assert.Equal(t, "START", progress.Action)
	})

	t.Run("phase stop", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.PhaseEvent{
				Phase:  "boot",
				Action: machine.PhaseEvent_STOP,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "boot", progress.Phase)
		assert.Equal(t, "STOP", progress.Action)
	})
}

func TestParseEvent_TaskEvent(t *testing.T) {
	c := &Client{}

	t.Run("task start", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.TaskEvent{
				Task:   "downloading image",
				Action: machine.TaskEvent_START,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "downloading image", progress.Task)
		assert.Equal(t, "START", progress.Action)
	})

	t.Run("task stop", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.TaskEvent{
				Task:   "writing disk",
				Action: machine.TaskEvent_STOP,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "writing disk", progress.Task)
		assert.Equal(t, "STOP", progress.Action)
	})
}

func TestParseEvent_MachineStatusEvent(t *testing.T) {
	c := &Client{}

	t.Run("running state", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.MachineStatusEvent{
				Stage: machine.MachineStatusEvent_RUNNING,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "running", progress.Stage)
		// Note: parseEvent no longer sets Done - caller decides based on context
		assert.False(t, progress.Done)
	})

	t.Run("booting state", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.MachineStatusEvent{
				Stage: machine.MachineStatusEvent_BOOTING,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "booting", progress.Stage)
		assert.False(t, progress.Done)
	})

	t.Run("upgrading state", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.MachineStatusEvent{
				Stage: machine.MachineStatusEvent_UPGRADING,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "upgrading", progress.Stage)
		assert.False(t, progress.Done)
	})

	t.Run("rebooting state", func(t *testing.T) {
		event := talosclient.Event{
			Payload: &machine.MachineStatusEvent{
				Stage: machine.MachineStatusEvent_REBOOTING,
			},
		}
		progress := c.parseEvent(event)
		require.NotNil(t, progress)
		assert.Equal(t, "rebooting", progress.Stage)
		assert.False(t, progress.Done)
	})
}

func TestParseEvent_UnknownPayload(t *testing.T) {
	c := &Client{}

	t.Run("unknown proto message returns nil", func(t *testing.T) {
		// Use a proto message type that isn't handled in the switch
		event := talosclient.Event{
			Payload: &machine.Version{},
		}
		progress := c.parseEvent(event)
		assert.Nil(t, progress)
	})

	t.Run("nil payload returns nil", func(t *testing.T) {
		event := talosclient.Event{
			Payload: nil,
		}
		progress := c.parseEvent(event)
		assert.Nil(t, progress)
	})
}

// ============================================================================
// K8s Client Tests (using fake clientset)
// ============================================================================

// makeNode creates a K8s Node for testing
func makeNode(name, ip string, ready bool) *corev1.Node {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: ip},
			},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: status},
			},
		},
	}
}

func TestIsK8sNodeReady(t *testing.T) {
	ctx := context.Background()

	t.Run("nil k8s client returns true", func(t *testing.T) {
		c := &Client{k8s: nil}
		assert.True(t, c.isK8sNodeReady(ctx, "192.168.1.1"))
	})

	t.Run("node ready", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset(makeNode("node1", "192.168.1.1", true))
		c := &Client{k8s: fakeClient}
		assert.True(t, c.isK8sNodeReady(ctx, "192.168.1.1"))
	})

	t.Run("node not ready", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset(makeNode("node1", "192.168.1.1", false))
		c := &Client{k8s: fakeClient}
		assert.False(t, c.isK8sNodeReady(ctx, "192.168.1.1"))
	})

	t.Run("node not found", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset(makeNode("node1", "192.168.1.99", true))
		c := &Client{k8s: fakeClient}
		assert.False(t, c.isK8sNodeReady(ctx, "192.168.1.1"))
	})

	t.Run("empty cluster", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		c := &Client{k8s: fakeClient}
		assert.False(t, c.isK8sNodeReady(ctx, "192.168.1.1"))
	})

	t.Run("multiple nodes finds correct one", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset(
			makeNode("node1", "192.168.1.1", false),
			makeNode("node2", "192.168.1.2", true),
			makeNode("node3", "192.168.1.3", true),
		)
		c := &Client{k8s: fakeClient}
		assert.False(t, c.isK8sNodeReady(ctx, "192.168.1.1"))
		assert.True(t, c.isK8sNodeReady(ctx, "192.168.1.2"))
		assert.True(t, c.isK8sNodeReady(ctx, "192.168.1.3"))
	})
}

func TestGetK8sNodeName(t *testing.T) {
	ctx := context.Background()

	t.Run("nil k8s client returns empty", func(t *testing.T) {
		c := &Client{k8s: nil}
		assert.Equal(t, "", c.GetK8sNodeName(ctx, "192.168.1.1"))
	})

	t.Run("node found", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset(makeNode("my-node", "192.168.1.1", true))
		c := &Client{k8s: fakeClient}
		assert.Equal(t, "my-node", c.GetK8sNodeName(ctx, "192.168.1.1"))
	})

	t.Run("node not found", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset(makeNode("node1", "192.168.1.99", true))
		c := &Client{k8s: fakeClient}
		assert.Equal(t, "", c.GetK8sNodeName(ctx, "192.168.1.1"))
	})

	t.Run("multiple nodes finds correct one", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset(
			makeNode("controlplane-1", "192.168.1.1", true),
			makeNode("worker-1", "192.168.1.2", true),
			makeNode("worker-2", "192.168.1.3", true),
		)
		c := &Client{k8s: fakeClient}
		assert.Equal(t, "controlplane-1", c.GetK8sNodeName(ctx, "192.168.1.1"))
		assert.Equal(t, "worker-1", c.GetK8sNodeName(ctx, "192.168.1.2"))
		assert.Equal(t, "worker-2", c.GetK8sNodeName(ctx, "192.168.1.3"))
		assert.Equal(t, "", c.GetK8sNodeName(ctx, "192.168.1.99"))
	})
}

// ============================================================================
// SDK-Dependent Function Tests (using MockTalosMachineClient)
// ============================================================================

func TestClient_GetVersion(t *testing.T) {
	ctx := context.Background()

	t.Run("success with v prefix", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: &machine.VersionInfo{Tag: "v1.8.0"}},
					},
				}, nil
			},
		}
		c := &Client{talos: mockTalos}
		version, err := c.GetVersion(ctx, "192.168.1.1")
		require.NoError(t, err)
		assert.Equal(t, "1.8.0", version)
	})

	t.Run("success without v prefix", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: &machine.VersionInfo{Tag: "1.7.5"}},
					},
				}, nil
			},
		}
		c := &Client{talos: mockTalos}
		version, err := c.GetVersion(ctx, "192.168.1.1")
		require.NoError(t, err)
		assert.Equal(t, "1.7.5", version)
	})

	t.Run("error from SDK", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return nil, errors.New("connection refused")
			},
		}
		c := &Client{talos: mockTalos}
		_, err := c.GetVersion(ctx, "192.168.1.1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get version")
	})

	t.Run("empty messages", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{Messages: []*machine.Version{}}, nil
			},
		}
		c := &Client{talos: mockTalos}
		_, err := c.GetVersion(ctx, "192.168.1.1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no version in response")
	})

	t.Run("nil version in message", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: nil},
					},
				}, nil
			},
		}
		c := &Client{talos: mockTalos}
		_, err := c.GetVersion(ctx, "192.168.1.1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no version in response")
	})

	t.Run("empty tag", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: &machine.VersionInfo{Tag: ""}},
					},
				}, nil
			},
		}
		c := &Client{talos: mockTalos}
		version, err := c.GetVersion(ctx, "192.168.1.1")
		require.NoError(t, err)
		assert.Equal(t, "", version)
	})
}

func TestClient_GetMachineType(t *testing.T) {
	ctx := context.Background()

	t.Run("returns unknown by design", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{
							Version:  &machine.VersionInfo{Tag: "v1.7.0"},
							Platform: &machine.PlatformInfo{Name: "metal"},
						},
					},
				}, nil
			},
		}
		c := &Client{talos: mockTalos}
		machineType, err := c.GetMachineType(ctx, "192.168.1.1")
		require.NoError(t, err)
		assert.Equal(t, "unknown", machineType)
	})

	t.Run("error from SDK", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return nil, errors.New("connection refused")
			},
		}
		c := &Client{talos: mockTalos}
		_, err := c.GetMachineType(ctx, "192.168.1.1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get version")
	})
}

func TestClient_IsReachable(t *testing.T) {
	ctx := context.Background()

	t.Run("reachable node", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: &machine.VersionInfo{Tag: "v1.7.0"}},
					},
				}, nil
			},
		}
		c := &Client{talos: mockTalos}
		assert.True(t, c.IsReachable(ctx, "192.168.1.1"))
	})

	t.Run("unreachable node", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return nil, errors.New("connection refused")
			},
		}
		c := &Client{talos: mockTalos}
		assert.False(t, c.IsReachable(ctx, "192.168.1.1"))
	})
}

func TestClient_Upgrade(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			UpgradeWithOptionsFunc: func(ctx context.Context, opts ...talosclient.UpgradeOption) (*machine.UpgradeResponse, error) {
				return &machine.UpgradeResponse{}, nil
			},
		}
		c := &Client{talos: mockTalos}
		err := c.Upgrade(ctx, "192.168.1.1", "ghcr.io/siderolabs/installer:v1.8.0", true)
		require.NoError(t, err)
	})

	t.Run("error from SDK", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			UpgradeWithOptionsFunc: func(ctx context.Context, opts ...talosclient.UpgradeOption) (*machine.UpgradeResponse, error) {
				return nil, errors.New("upgrade in progress")
			},
		}
		c := &Client{talos: mockTalos}
		err := c.Upgrade(ctx, "192.168.1.1", "ghcr.io/siderolabs/installer:v1.8.0", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upgrade failed")
	})
}

func TestClient_Close(t *testing.T) {
	t.Run("nil talos client", func(t *testing.T) {
		c := &Client{talos: nil}
		err := c.Close()
		require.NoError(t, err)
	})

	t.Run("close success", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			CloseFunc: func() error {
				return nil
			},
		}
		c := &Client{talos: mockTalos}
		err := c.Close()
		require.NoError(t, err)
	})

	t.Run("close error", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			CloseFunc: func() error {
				return errors.New("close failed")
			},
		}
		c := &Client{talos: mockTalos}
		err := c.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "close failed")
	})
}

func TestClient_GetNodeStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("reachable node with version", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: &machine.VersionInfo{Tag: "v1.8.0"}},
					},
				}, nil
			},
		}
		c := &Client{talos: mockTalos, k8s: nil}
		status := c.GetNodeStatus(ctx, "192.168.1.1", "amd64-intel", "controlplane", true)

		assert.Equal(t, "192.168.1.1", status.IP)
		assert.Equal(t, "amd64-intel", status.Profile)
		assert.Equal(t, "controlplane", status.Role)
		assert.Equal(t, "1.8.0", status.Version)
		assert.Equal(t, "unknown", status.MachineType)
		assert.True(t, status.Secureboot)
		assert.True(t, status.Reachable)
	})

	t.Run("unreachable node", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return nil, errors.New("connection refused")
			},
		}
		c := &Client{talos: mockTalos, k8s: nil}
		status := c.GetNodeStatus(ctx, "192.168.1.1", "amd64-intel", "worker", false)

		assert.Equal(t, "192.168.1.1", status.IP)
		assert.Equal(t, "amd64-intel", status.Profile)
		assert.Equal(t, "worker", status.Role)
		assert.Equal(t, "N/A", status.Version)
		assert.Equal(t, "unknown", status.MachineType)
		assert.False(t, status.Secureboot)
		assert.False(t, status.Reachable)
	})
}

// ============================================================================
// MockTalosMachineClient Tests
// ============================================================================

func TestMockTalosMachineClient_DefaultBehavior(t *testing.T) {
	mock := &MockTalosMachineClient{}
	ctx := context.Background()

	t.Run("Close returns nil", func(t *testing.T) {
		err := mock.Close()
		assert.NoError(t, err)
	})

	t.Run("Version returns default", func(t *testing.T) {
		resp, err := mock.Version(ctx)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Messages, 1)
		assert.Equal(t, "v1.7.0", resp.Messages[0].Version.Tag)
	})

	t.Run("UpgradeWithOptions returns empty response", func(t *testing.T) {
		resp, err := mock.UpgradeWithOptions(ctx)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ServiceInfo returns empty slice", func(t *testing.T) {
		resp, err := mock.ServiceInfo(ctx, "etcd")
		require.NoError(t, err)
		assert.Empty(t, resp)
	})

	t.Run("EventsWatchV2 returns nil", func(t *testing.T) {
		eventCh := make(chan talosclient.EventResult, 10)
		err := mock.EventsWatchV2(ctx, eventCh)
		assert.NoError(t, err)
	})

	t.Run("COSIList returns empty list", func(t *testing.T) {
		list, err := mock.COSIList(ctx, resource.NewMetadata("test", "type", "id", resource.VersionUndefined))
		require.NoError(t, err)
		assert.Empty(t, list.Items)
	})
}

func TestMockTalosMachineClient_ImplementsInterface(t *testing.T) {
	var client TalosMachineClient = &MockTalosMachineClient{}
	assert.NotNil(t, client)
}

// ============================================================================
// Clock-Based Tests for Timeout Functions
// ============================================================================

func TestClient_WaitForNode(t *testing.T) {
	ctx := context.Background()

	t.Run("returns immediately when node is reachable", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: &machine.VersionInfo{Tag: "v1.7.0"}},
					},
				}, nil
			},
		}
		mockClock := NewMockClock(time.Now())

		c := &Client{talos: mockTalos, k8s: nil, clock: mockClock}
		err := c.WaitForNode(ctx, "192.168.1.1", time.Minute)
		require.NoError(t, err)
	})

	t.Run("times out when node is unreachable", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return nil, errors.New("connection refused")
			},
		}
		startTime := time.Now()
		mockClock := &MockClock{
			CurrentTime:    startTime,
			AdvanceOnAfter: true,
		}
		// Mock clock that advances time on each Sleep call
		sleepCount := 0
		mockClock.SleepFunc = func(d time.Duration) {
			sleepCount++
			mockClock.CurrentTime = mockClock.CurrentTime.Add(d)
		}

		c := &Client{talos: mockTalos, k8s: nil, clock: mockClock}
		err := c.WaitForNode(ctx, "192.168.1.1", 30*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout waiting for node")
		// Verify multiple retries occurred (5 second sleep, 30 second timeout = ~6 retries)
		assert.GreaterOrEqual(t, sleepCount, 5, "expected at least 5 sleep calls")
	})

	t.Run("context cancellation", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return nil, errors.New("connection refused")
			},
		}
		mockClock := NewMockClock(time.Now())

		c := &Client{talos: mockTalos, k8s: nil, clock: mockClock}

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		err := c.WaitForNode(canceledCtx, "192.168.1.1", time.Minute)
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("node reachable but k8s not ready then becomes ready", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: &machine.VersionInfo{Tag: "v1.7.0"}},
					},
				}, nil
			},
		}

		// Start with unready node, then make it ready
		k8sReady := false
		fakeClient := fake.NewSimpleClientset(makeNode("node1", "192.168.1.1", false))

		mockClock := NewMockClock(time.Now())
		mockClock.SleepFunc = func(d time.Duration) {
			mockClock.CurrentTime = mockClock.CurrentTime.Add(d)
			// After first sleep, make k8s node ready
			if !k8sReady {
				k8sReady = true
				// Update the fake client's node to be ready
				node := makeNode("node1", "192.168.1.1", true)
				fakeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
			}
		}

		c := &Client{talos: mockTalos, k8s: fakeClient, clock: mockClock}
		err := c.WaitForNode(ctx, "192.168.1.1", time.Minute)
		require.NoError(t, err)
	})
}

func TestClient_WaitForServices(t *testing.T) {
	ctx := context.Background()

	t.Run("returns immediately when all services are healthy", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			ServiceInfoFunc: func(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
				return []talosclient.ServiceInfo{
					{
						Service: &machine.ServiceInfo{
							Id:     service,
							State:  "Running",
							Health: &machine.ServiceHealth{Healthy: true},
						},
					},
				}, nil
			},
		}
		mockClock := NewMockClock(time.Now())

		c := &Client{talos: mockTalos, clock: mockClock}
		err := c.WaitForServices(ctx, "192.168.1.1", []string{"etcd", "kubelet"}, time.Minute)
		require.NoError(t, err)
	})

	t.Run("times out when service is unhealthy", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			ServiceInfoFunc: func(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
				return []talosclient.ServiceInfo{
					{
						Service: &machine.ServiceInfo{
							Id:     service,
							State:  "Running",
							Health: &machine.ServiceHealth{Healthy: false},
						},
					},
				}, nil
			},
		}
		startTime := time.Now()
		mockClock := &MockClock{CurrentTime: startTime}
		mockClock.SleepFunc = func(d time.Duration) {
			mockClock.CurrentTime = mockClock.CurrentTime.Add(d)
		}

		c := &Client{talos: mockTalos, clock: mockClock}
		err := c.WaitForServices(ctx, "192.168.1.1", []string{"etcd"}, 10*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout waiting for services")
	})

	t.Run("times out when service is not running", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			ServiceInfoFunc: func(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
				return []talosclient.ServiceInfo{
					{
						Service: &machine.ServiceInfo{
							Id:     service,
							State:  "Starting",
							Health: nil,
						},
					},
				}, nil
			},
		}
		startTime := time.Now()
		mockClock := &MockClock{CurrentTime: startTime}
		mockClock.SleepFunc = func(d time.Duration) {
			mockClock.CurrentTime = mockClock.CurrentTime.Add(d)
		}

		c := &Client{talos: mockTalos, clock: mockClock}
		err := c.WaitForServices(ctx, "192.168.1.1", []string{"etcd"}, 10*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout waiting for services")
	})

	t.Run("times out when service info errors", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			ServiceInfoFunc: func(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
				return nil, errors.New("service not found")
			},
		}
		startTime := time.Now()
		mockClock := &MockClock{CurrentTime: startTime}
		mockClock.SleepFunc = func(d time.Duration) {
			mockClock.CurrentTime = mockClock.CurrentTime.Add(d)
		}

		c := &Client{talos: mockTalos, clock: mockClock}
		err := c.WaitForServices(ctx, "192.168.1.1", []string{"etcd"}, 10*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout waiting for services")
	})

	t.Run("context cancellation", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			ServiceInfoFunc: func(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
				return nil, errors.New("service not found")
			},
		}
		mockClock := NewMockClock(time.Now())

		c := &Client{talos: mockTalos, clock: mockClock}

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		err := c.WaitForServices(canceledCtx, "192.168.1.1", []string{"etcd"}, time.Minute)
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("service becomes healthy after retry", func(t *testing.T) {
		attempts := 0
		mockTalos := &MockTalosMachineClient{
			ServiceInfoFunc: func(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
				attempts++
				if attempts < 3 {
					return []talosclient.ServiceInfo{
						{
							Service: &machine.ServiceInfo{
								Id:     service,
								State:  "Starting",
								Health: nil,
							},
						},
					}, nil
				}
				return []talosclient.ServiceInfo{
					{
						Service: &machine.ServiceInfo{
							Id:     service,
							State:  "Running",
							Health: &machine.ServiceHealth{Healthy: true},
						},
					},
				}, nil
			},
		}
		mockClock := NewMockClock(time.Now())
		mockClock.SleepFunc = func(d time.Duration) {
			mockClock.CurrentTime = mockClock.CurrentTime.Add(d)
		}

		c := &Client{talos: mockTalos, clock: mockClock}
		err := c.WaitForServices(ctx, "192.168.1.1", []string{"etcd"}, time.Minute)
		require.NoError(t, err)
		assert.Equal(t, 3, attempts)
	})
}

// ============================================================================
// MockClock Tests
// ============================================================================

func TestMockClock(t *testing.T) {
	t.Run("Now returns CurrentTime", func(t *testing.T) {
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		clock := NewMockClock(now)
		assert.Equal(t, now, clock.Now())
	})

	t.Run("Advance moves time forward", func(t *testing.T) {
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		clock := NewMockClock(now)
		clock.Advance(time.Hour)
		assert.Equal(t, now.Add(time.Hour), clock.Now())
	})

	t.Run("Sleep advances time", func(t *testing.T) {
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		clock := NewMockClock(now)
		clock.Sleep(5 * time.Second)
		assert.Equal(t, now.Add(5*time.Second), clock.Now())
	})

	t.Run("After returns immediately with value", func(t *testing.T) {
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		clock := NewMockClock(now)

		ch := clock.After(time.Second)
		select {
		case receivedTime := <-ch:
			assert.Equal(t, now, receivedTime)
		default:
			assert.Fail(t, "After should return immediately in mock")
		}
	})

	t.Run("AdvanceOnAfter option", func(t *testing.T) {
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		clock := &MockClock{
			CurrentTime:    now,
			AdvanceOnAfter: true,
		}

		clock.After(5 * time.Second)
		assert.Equal(t, now.Add(5*time.Second), clock.Now())
	})

	t.Run("After with custom AfterFunc", func(t *testing.T) {
		customTime := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
		clock := &MockClock{
			CurrentTime: time.Now(),
			AfterFunc: func(d time.Duration) <-chan time.Time {
				ch := make(chan time.Time, 1)
				ch <- customTime
				return ch
			},
		}

		ch := clock.After(time.Second)
		select {
		case receivedTime := <-ch:
			assert.Equal(t, customTime, receivedTime)
		default:
			assert.Fail(t, "After should return immediately")
		}
	})
}

// Test nil clock fallback paths
func TestClient_NilClockFallback(t *testing.T) {
	ctx := context.Background()

	t.Run("WaitForServices with nil clock uses real clock", func(t *testing.T) {
		// This test verifies the nil clock fallback path
		// We use a very short timeout and a mock that returns healthy immediately
		mockTalos := &MockTalosMachineClient{
			ServiceInfoFunc: func(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
				return []talosclient.ServiceInfo{
					{
						Service: &machine.ServiceInfo{
							Id:     service,
							State:  "Running",
							Health: &machine.ServiceHealth{Healthy: true},
						},
					},
				}, nil
			},
		}
		// Note: clock is nil here - exercises the fallback
		c := &Client{talos: mockTalos, clock: nil}
		err := c.WaitForServices(ctx, "192.168.1.1", []string{"etcd"}, time.Second)
		require.NoError(t, err)
	})

	t.Run("WaitForNode with nil clock uses real clock", func(t *testing.T) {
		mockTalos := &MockTalosMachineClient{
			VersionFunc: func(ctx context.Context) (*machine.VersionResponse, error) {
				return &machine.VersionResponse{
					Messages: []*machine.Version{
						{Version: &machine.VersionInfo{Tag: "v1.7.0"}},
					},
				}, nil
			},
		}
		// Note: clock is nil here - exercises the fallback
		c := &Client{talos: mockTalos, k8s: nil, clock: nil}
		err := c.WaitForNode(ctx, "192.168.1.1", time.Second)
		require.NoError(t, err)
	})
}
