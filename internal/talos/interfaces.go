package talos

import (
	"context"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// TalosClientInterface defines the interface for interacting with Talos nodes.
// This interface enables mocking the Talos client for testing.
type TalosClientInterface interface {
	// Close closes the client connection
	Close() error

	// GetVersion retrieves the Talos version for a node
	GetVersion(ctx context.Context, nodeIP string) (string, error)

	// GetMachineType retrieves the machine type for a node
	GetMachineType(ctx context.Context, nodeIP string) (string, error)

	// IsReachable checks if a node is reachable via the Talos API
	IsReachable(ctx context.Context, nodeIP string) bool

	// Upgrade performs an upgrade on a node
	Upgrade(ctx context.Context, nodeIP, image string, preserve bool) error

	// WaitForNode waits for a node to be ready after upgrade
	WaitForNode(ctx context.Context, nodeIP string, timeout time.Duration) error

	// GetNodeStatus retrieves comprehensive status for a node
	GetNodeStatus(ctx context.Context, nodeIP, profile, role string, secureboot bool) NodeStatus

	// WatchUpgrade streams upgrade events
	WatchUpgrade(ctx context.Context, nodeIP string, timeout time.Duration, onProgress ProgressCallback) error

	// WaitForServices waits for critical Talos services to be healthy
	WaitForServices(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error

	// WaitForStaticPods waits for K8s control plane static pods to be healthy
	WaitForStaticPods(ctx context.Context, nodeIP string, timeout time.Duration) error
}

// Ensure Client implements TalosClientInterface
var _ TalosClientInterface = (*Client)(nil)

// MockClient is a mock implementation of TalosClientInterface for testing
type MockClient struct {
	// GetVersionFunc is the mock implementation of GetVersion
	GetVersionFunc func(ctx context.Context, nodeIP string) (string, error)

	// GetMachineTypeFunc is the mock implementation of GetMachineType
	GetMachineTypeFunc func(ctx context.Context, nodeIP string) (string, error)

	// IsReachableFunc is the mock implementation of IsReachable
	IsReachableFunc func(ctx context.Context, nodeIP string) bool

	// UpgradeFunc is the mock implementation of Upgrade
	UpgradeFunc func(ctx context.Context, nodeIP, image string, preserve bool) error

	// WaitForNodeFunc is the mock implementation of WaitForNode
	WaitForNodeFunc func(ctx context.Context, nodeIP string, timeout time.Duration) error

	// GetNodeStatusFunc is the mock implementation of GetNodeStatus
	GetNodeStatusFunc func(ctx context.Context, nodeIP, profile, role string, secureboot bool) NodeStatus

	// WatchUpgradeFunc is the mock implementation of WatchUpgrade
	WatchUpgradeFunc func(ctx context.Context, nodeIP string, timeout time.Duration, onProgress ProgressCallback) error

	// WaitForServicesFunc is the mock implementation of WaitForServices
	WaitForServicesFunc func(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error

	// WaitForStaticPodsFunc is the mock implementation of WaitForStaticPods
	WaitForStaticPodsFunc func(ctx context.Context, nodeIP string, timeout time.Duration) error
}

func (m *MockClient) Close() error {
	return nil
}

func (m *MockClient) GetVersion(ctx context.Context, nodeIP string) (string, error) {
	if m.GetVersionFunc != nil {
		return m.GetVersionFunc(ctx, nodeIP)
	}
	return "1.7.0", nil
}

func (m *MockClient) GetMachineType(ctx context.Context, nodeIP string) (string, error) {
	if m.GetMachineTypeFunc != nil {
		return m.GetMachineTypeFunc(ctx, nodeIP)
	}
	return "unknown", nil
}

func (m *MockClient) IsReachable(ctx context.Context, nodeIP string) bool {
	if m.IsReachableFunc != nil {
		return m.IsReachableFunc(ctx, nodeIP)
	}
	return true
}

func (m *MockClient) Upgrade(ctx context.Context, nodeIP, image string, preserve bool) error {
	if m.UpgradeFunc != nil {
		return m.UpgradeFunc(ctx, nodeIP, image, preserve)
	}
	return nil
}

func (m *MockClient) WaitForNode(ctx context.Context, nodeIP string, timeout time.Duration) error {
	if m.WaitForNodeFunc != nil {
		return m.WaitForNodeFunc(ctx, nodeIP, timeout)
	}
	return nil
}

func (m *MockClient) GetNodeStatus(ctx context.Context, nodeIP, profile, role string, secureboot bool) NodeStatus {
	if m.GetNodeStatusFunc != nil {
		return m.GetNodeStatusFunc(ctx, nodeIP, profile, role, secureboot)
	}
	return NodeStatus{
		IP:          nodeIP,
		Profile:     profile,
		Role:        role,
		Version:     "1.7.0",
		MachineType: "unknown",
		Secureboot:  secureboot,
		Reachable:   true,
	}
}

func (m *MockClient) WatchUpgrade(ctx context.Context, nodeIP string, timeout time.Duration, onProgress ProgressCallback) error {
	if m.WatchUpgradeFunc != nil {
		return m.WatchUpgradeFunc(ctx, nodeIP, timeout, onProgress)
	}
	return nil
}

func (m *MockClient) WaitForServices(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error {
	if m.WaitForServicesFunc != nil {
		return m.WaitForServicesFunc(ctx, nodeIP, services, timeout)
	}
	return nil
}

func (m *MockClient) WaitForStaticPods(ctx context.Context, nodeIP string, timeout time.Duration) error {
	if m.WaitForStaticPodsFunc != nil {
		return m.WaitForStaticPodsFunc(ctx, nodeIP, timeout)
	}
	return nil
}

// MockTalosMachineClient is a mock implementation of TalosMachineClient for testing
// SDK-dependent functions in the Client struct.
type MockTalosMachineClient struct {
	// CloseFunc is the mock implementation of Close
	CloseFunc func() error

	// VersionFunc is the mock implementation of Version
	VersionFunc func(ctx context.Context) (*machine.VersionResponse, error)

	// UpgradeWithOptionsFunc is the mock implementation of UpgradeWithOptions
	UpgradeWithOptionsFunc func(ctx context.Context, opts ...talosclient.UpgradeOption) (*machine.UpgradeResponse, error)

	// ServiceInfoFunc is the mock implementation of ServiceInfo
	ServiceInfoFunc func(ctx context.Context, service string) ([]talosclient.ServiceInfo, error)

	// EventsWatchV2Func is the mock implementation of EventsWatchV2
	EventsWatchV2Func func(ctx context.Context, eventCh chan<- talosclient.EventResult, opts ...talosclient.EventsOptionFunc) error

	// COSIListFunc is the mock implementation of COSIList
	COSIListFunc func(ctx context.Context, md resource.Metadata) (resource.List, error)
}

// Ensure MockTalosMachineClient implements TalosMachineClient
var _ TalosMachineClient = (*MockTalosMachineClient)(nil)

func (m *MockTalosMachineClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockTalosMachineClient) Version(ctx context.Context) (*machine.VersionResponse, error) {
	if m.VersionFunc != nil {
		return m.VersionFunc(ctx)
	}
	return &machine.VersionResponse{
		Messages: []*machine.Version{
			{
				Version: &machine.VersionInfo{Tag: "v1.7.0"},
			},
		},
	}, nil
}

func (m *MockTalosMachineClient) UpgradeWithOptions(ctx context.Context, opts ...talosclient.UpgradeOption) (*machine.UpgradeResponse, error) {
	if m.UpgradeWithOptionsFunc != nil {
		return m.UpgradeWithOptionsFunc(ctx, opts...)
	}
	return &machine.UpgradeResponse{}, nil
}

func (m *MockTalosMachineClient) ServiceInfo(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
	if m.ServiceInfoFunc != nil {
		return m.ServiceInfoFunc(ctx, service)
	}
	return []talosclient.ServiceInfo{}, nil
}

func (m *MockTalosMachineClient) EventsWatchV2(ctx context.Context, eventCh chan<- talosclient.EventResult, opts ...talosclient.EventsOptionFunc) error {
	if m.EventsWatchV2Func != nil {
		return m.EventsWatchV2Func(ctx, eventCh, opts...)
	}
	return nil
}

func (m *MockTalosMachineClient) COSIList(ctx context.Context, md resource.Metadata) (resource.List, error) {
	if m.COSIListFunc != nil {
		return m.COSIListFunc(ctx, md)
	}
	return resource.List{}, nil
}
