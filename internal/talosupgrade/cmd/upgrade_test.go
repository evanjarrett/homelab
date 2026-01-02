package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/evanjarrett/homelab/internal/talosupgrade/config"
	"github.com/evanjarrett/homelab/internal/talosupgrade/factory"
	"github.com/evanjarrett/homelab/internal/talosupgrade/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// isVersion() Tests
// ============================================================================

func TestIsVersion_ValidVersions(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"1.7.0", true},
		{"1.12.0", true},
		{"2.0.0", true},
		{"1.7", true},  // Two-part version
		{"1", true},    // Single number
		{"10.20.30", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isVersion(tt.input)
			assert.Equal(t, tt.expected, result, "isVersion(%q)", tt.input)
		})
	}
}

func TestIsVersion_IPAddresses(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"192.168.1.1", false},   // 3 dots = IP
		{"192.168.1.161", false},
		{"10.0.0.1", false},
		{"172.16.0.100", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isVersion(tt.input)
			assert.Equal(t, tt.expected, result, "isVersion(%q) should detect IP", tt.input)
		})
	}
}

func TestIsVersion_InvalidInputs(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", false},           // Empty string
		{"all", false},        // Target keyword
		{"workers", false},    // Target keyword
		{"controlplanes", false},
		{"v1.7.0", false},     // Starts with letter
		{"profile-name", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isVersion(tt.input)
			assert.Equal(t, tt.expected, result, "isVersion(%q)", tt.input)
		})
	}
}

// ============================================================================
// parseUpgradeArgs() Tests
// ============================================================================

func TestParseUpgradeArgs_NoArgs(t *testing.T) {
	target, version := parseUpgradeArgs([]string{})
	assert.Equal(t, "all", target)
	assert.Empty(t, version)
}

func TestParseUpgradeArgs_VersionOnly(t *testing.T) {
	target, version := parseUpgradeArgs([]string{"1.7.0"})
	assert.Equal(t, "all", target)
	assert.Equal(t, "1.7.0", version)
}

func TestParseUpgradeArgs_TargetOnly(t *testing.T) {
	target, version := parseUpgradeArgs([]string{"workers"})
	assert.Equal(t, "workers", target)
	assert.Empty(t, version)
}

func TestParseUpgradeArgs_VersionThenTarget(t *testing.T) {
	// Version comes first
	target, version := parseUpgradeArgs([]string{"1.7.0", "workers"})
	assert.Equal(t, "workers", target)
	assert.Equal(t, "1.7.0", version)
}

func TestParseUpgradeArgs_TargetThenVersion(t *testing.T) {
	// Target comes first
	target, version := parseUpgradeArgs([]string{"workers", "1.7.0"})
	assert.Equal(t, "workers", target)
	assert.Equal(t, "1.7.0", version)
}

func TestParseUpgradeArgs_IPAsTarget(t *testing.T) {
	// IP address should be recognized as target, not version
	target, version := parseUpgradeArgs([]string{"192.168.1.161"})
	assert.Equal(t, "192.168.1.161", target)
	assert.Empty(t, version)
}

func TestParseUpgradeArgs_VersionAndIP(t *testing.T) {
	// Version first, then IP
	target, version := parseUpgradeArgs([]string{"1.7.0", "192.168.1.161"})
	assert.Equal(t, "192.168.1.161", target)
	assert.Equal(t, "1.7.0", version)
}

func TestParseUpgradeArgs_IPAndVersion(t *testing.T) {
	// IP first, then version
	target, version := parseUpgradeArgs([]string{"192.168.1.161", "1.7.0"})
	assert.Equal(t, "192.168.1.161", target)
	assert.Equal(t, "1.7.0", version)
}

func TestParseUpgradeArgs_ProfileTarget(t *testing.T) {
	target, version := parseUpgradeArgs([]string{"arm64-rpi", "1.8.0"})
	assert.Equal(t, "arm64-rpi", target)
	assert.Equal(t, "1.8.0", version)
}

// ============================================================================
// getNodesForTarget() Tests
// ============================================================================

func setupTestConfig() {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 600,
		},
		Profiles: map[string]config.Profile{
			"profile-a": {Arch: "amd64", Platform: "metal"},
			"profile-b": {Arch: "arm64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleControlPlane},
			{IP: "192.168.1.2", Profile: "profile-a", Role: config.RoleWorker},
			{IP: "192.168.1.3", Profile: "profile-b", Role: config.RoleWorker},
			{IP: "192.168.1.4", Profile: "profile-b", Role: config.RoleControlPlane},
		},
	}
}

func TestGetNodesForTarget_All(t *testing.T) {
	setupTestConfig()

	nodes, err := getNodesForTarget("all")
	require.NoError(t, err)
	assert.Len(t, nodes, 4)

	// Workers should come first (GetAllNodesOrdered)
	assert.Equal(t, config.RoleWorker, nodes[0].Role)
	assert.Equal(t, config.RoleWorker, nodes[1].Role)
	assert.Equal(t, config.RoleControlPlane, nodes[2].Role)
	assert.Equal(t, config.RoleControlPlane, nodes[3].Role)
}

func TestGetNodesForTarget_Workers(t *testing.T) {
	setupTestConfig()

	nodes, err := getNodesForTarget("workers")
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	for _, node := range nodes {
		assert.Equal(t, config.RoleWorker, node.Role)
	}
}

func TestGetNodesForTarget_ControlPlanes(t *testing.T) {
	setupTestConfig()

	nodes, err := getNodesForTarget("controlplanes")
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	for _, node := range nodes {
		assert.Equal(t, config.RoleControlPlane, node.Role)
	}
}

func TestGetNodesForTarget_ByIP(t *testing.T) {
	setupTestConfig()

	nodes, err := getNodesForTarget("192.168.1.2")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "192.168.1.2", nodes[0].IP)
	assert.Equal(t, "profile-a", nodes[0].Profile)
}

func TestGetNodesForTarget_ByProfile(t *testing.T) {
	setupTestConfig()

	nodes, err := getNodesForTarget("profile-b")
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	for _, node := range nodes {
		assert.Equal(t, "profile-b", node.Profile)
	}
}

func TestGetNodesForTarget_UnknownTarget(t *testing.T) {
	setupTestConfig()

	nodes, err := getNodesForTarget("unknown-target")
	assert.Error(t, err)
	assert.Nil(t, nodes)
	assert.Contains(t, err.Error(), "unknown target")
}

func TestGetNodesForTarget_NonexistentIP(t *testing.T) {
	setupTestConfig()

	// IP with 3 dots but not in config - treated as profile name search
	nodes, err := getNodesForTarget("192.168.1.99")
	assert.Error(t, err)
	assert.Nil(t, nodes)
}

// ============================================================================
// upgradeNode() Tests
// ============================================================================

func TestUpgradeNode_AlreadyAtVersion(t *testing.T) {
	setupTestConfig()
	cfg.Settings.DefaultTimeoutSeconds = 60
	dryRun = false
	preserve = true

	mock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.9.0", nil // Same as target
		},
	}

	node := config.Node{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleWorker}
	skipped, err := upgradeNode(context.Background(), mock, node, "factory.talos.dev/installer/abc:v1.9.0", "1.9.0")

	require.NoError(t, err)
	assert.True(t, skipped, "should skip node already at target version")
}

func TestUpgradeNode_DryRun(t *testing.T) {
	setupTestConfig()
	cfg.Settings.DefaultTimeoutSeconds = 60
	dryRun = true
	preserve = true

	upgradeWasCalled := false
	mock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil // Old version
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, preserve bool) error {
			upgradeWasCalled = true
			return nil
		},
	}

	node := config.Node{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleWorker}
	skipped, err := upgradeNode(context.Background(), mock, node, "factory.talos.dev/installer/abc:v1.9.0", "1.9.0")

	require.NoError(t, err)
	assert.False(t, skipped)
	assert.False(t, upgradeWasCalled, "upgrade should not be called in dry run mode")

	// Reset for other tests
	dryRun = false
}

func TestUpgradeNode_Success_Worker(t *testing.T) {
	setupTestConfig()
	cfg.Settings.DefaultTimeoutSeconds = 60
	dryRun = false
	preserve = true

	upgradeCalledWith := ""
	mock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			// First call returns old version, second call returns new version
			if upgradeCalledWith != "" {
				return "1.9.0", nil
			}
			return "1.8.0", nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			upgradeCalledWith = image
			assert.Equal(t, "192.168.1.2", nodeIP)
			assert.True(t, pres)
			return nil
		},
		WatchUpgradeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration, cb talos.ProgressCallback) error {
			// Simulate progress
			if cb != nil {
				cb(talos.UpgradeProgress{Stage: "upgrading"})
				cb(talos.UpgradeProgress{Done: true})
			}
			return nil
		},
		WaitForServicesFunc: func(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error {
			// Worker services
			assert.Contains(t, services, "kubelet")
			return nil
		},
	}

	node := config.Node{IP: "192.168.1.2", Profile: "profile-a", Role: config.RoleWorker}
	skipped, err := upgradeNode(context.Background(), mock, node, "factory.talos.dev/installer/abc:v1.9.0", "1.9.0")

	require.NoError(t, err)
	assert.False(t, skipped)
	assert.Equal(t, "factory.talos.dev/installer/abc:v1.9.0", upgradeCalledWith)
}

func TestUpgradeNode_Success_ControlPlane(t *testing.T) {
	setupTestConfig()
	cfg.Settings.DefaultTimeoutSeconds = 60
	dryRun = false
	preserve = true

	waitForStaticPodsCalled := false
	mock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			return nil
		},
		WatchUpgradeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration, cb talos.ProgressCallback) error {
			return nil
		},
		WaitForServicesFunc: func(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error {
			// Control plane services should include etcd
			assert.Contains(t, services, "etcd")
			return nil
		},
		WaitForStaticPodsFunc: func(ctx context.Context, nodeIP string, timeout time.Duration) error {
			waitForStaticPodsCalled = true
			return nil
		},
	}

	node := config.Node{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleControlPlane}
	skipped, err := upgradeNode(context.Background(), mock, node, "factory.talos.dev/installer/abc:v1.9.0", "1.9.0")

	require.NoError(t, err)
	assert.False(t, skipped)
	assert.True(t, waitForStaticPodsCalled, "should wait for static pods on control plane")
}

func TestUpgradeNode_GetVersionError(t *testing.T) {
	setupTestConfig()
	cfg.Settings.DefaultTimeoutSeconds = 60
	dryRun = false
	preserve = true

	upgradeWasCalled := false
	mock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "", fmt.Errorf("connection refused")
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			upgradeWasCalled = true
			return nil
		},
		WatchUpgradeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration, cb talos.ProgressCallback) error {
			return nil
		},
		WaitForServicesFunc: func(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error {
			return nil
		},
	}

	node := config.Node{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleWorker}
	skipped, err := upgradeNode(context.Background(), mock, node, "factory.talos.dev/installer/abc:v1.9.0", "1.9.0")

	require.NoError(t, err)
	assert.False(t, skipped)
	assert.True(t, upgradeWasCalled, "should proceed with upgrade even if version check fails")
}

func TestUpgradeNode_UpgradeCommandFailure(t *testing.T) {
	setupTestConfig()
	cfg.Settings.DefaultTimeoutSeconds = 60
	dryRun = false
	preserve = true

	mock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			return fmt.Errorf("upgrade rejected: disk full")
		},
	}

	node := config.Node{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleWorker}
	skipped, err := upgradeNode(context.Background(), mock, node, "factory.talos.dev/installer/abc:v1.9.0", "1.9.0")

	require.Error(t, err)
	assert.False(t, skipped)
	assert.Contains(t, err.Error(), "upgrade command failed")
}

func TestUpgradeNode_WatchUpgradeFailure(t *testing.T) {
	setupTestConfig()
	cfg.Settings.DefaultTimeoutSeconds = 60
	dryRun = false
	preserve = true

	mock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			return nil
		},
		WatchUpgradeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration, cb talos.ProgressCallback) error {
			return fmt.Errorf("upgrade failed: kernel panic")
		},
	}

	node := config.Node{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleWorker}
	skipped, err := upgradeNode(context.Background(), mock, node, "factory.talos.dev/installer/abc:v1.9.0", "1.9.0")

	require.Error(t, err)
	assert.False(t, skipped)
	assert.Contains(t, err.Error(), "upgrade failed")
}

func TestUpgradeNode_ServiceWaitTimeout(t *testing.T) {
	setupTestConfig()
	cfg.Settings.DefaultTimeoutSeconds = 60
	dryRun = false
	preserve = true

	mock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			return nil
		},
		WatchUpgradeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration, cb talos.ProgressCallback) error {
			return nil
		},
		WaitForServicesFunc: func(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error {
			return fmt.Errorf("timeout waiting for services")
		},
	}

	node := config.Node{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleWorker}
	skipped, err := upgradeNode(context.Background(), mock, node, "factory.talos.dev/installer/abc:v1.9.0", "1.9.0")

	// Service wait timeout should log warning but not fail the upgrade
	require.NoError(t, err)
	assert.False(t, skipped)
}

// ============================================================================
// confirm() Tests
// ============================================================================

func TestConfirm_Yes(t *testing.T) {
	// Save and restore original reader
	origReader := confirmReader
	defer func() { confirmReader = origReader }()

	confirmReader = strings.NewReader("y\n")
	assert.True(t, confirm("Proceed?"))
}

func TestConfirm_YesFullWord(t *testing.T) {
	origReader := confirmReader
	defer func() { confirmReader = origReader }()

	confirmReader = strings.NewReader("yes\n")
	assert.True(t, confirm("Proceed?"))
}

func TestConfirm_YesUppercase(t *testing.T) {
	origReader := confirmReader
	defer func() { confirmReader = origReader }()

	confirmReader = strings.NewReader("Y\n")
	assert.True(t, confirm("Proceed?"))
}

func TestConfirm_No(t *testing.T) {
	origReader := confirmReader
	defer func() { confirmReader = origReader }()

	confirmReader = strings.NewReader("n\n")
	assert.False(t, confirm("Proceed?"))
}

func TestConfirm_Empty(t *testing.T) {
	origReader := confirmReader
	defer func() { confirmReader = origReader }()

	confirmReader = strings.NewReader("\n")
	assert.False(t, confirm("Proceed?"))
}

func TestConfirm_Invalid(t *testing.T) {
	origReader := confirmReader
	defer func() { confirmReader = origReader }()

	confirmReader = strings.NewReader("maybe\n")
	assert.False(t, confirm("Proceed?"))
}

// ============================================================================
// runURLs() Tests
// ============================================================================

func TestRunURLs_SingleProfile(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{
			"test-profile": {
				Arch:       "amd64",
				Platform:   "metal",
				Extensions: []string{"siderolabs/i915"},
			},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleWorker},
		},
	}

	err := runURLs("1.9.0")
	require.NoError(t, err)
}

func TestRunURLs_MultipleProfiles(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{
			"zebra-profile": {Arch: "amd64", Platform: "metal"},
			"alpha-profile": {Arch: "arm64", Platform: "metal"},
			"beta-profile":  {Arch: "amd64", Platform: "metal", Secureboot: true},
		},
		Nodes: []config.Node{},
	}

	err := runURLs("1.9.0")
	require.NoError(t, err)
}

func TestRunURLs_ProfileWithOverlay(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{
			"rpi-profile": {
				Arch:     "arm64",
				Platform: "metal",
				Overlay: &config.Overlay{
					Name:  "rpi_generic",
					Image: "siderolabs/sbc-raspberrypi",
				},
				Extensions: []string{"siderolabs/iscsi-tools"},
			},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.10", Profile: "rpi-profile", Role: config.RoleWorker},
		},
	}

	err := runURLs("1.9.0")
	require.NoError(t, err)
}

func TestRunURLs_ProfileWithKernelArgs(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{
			"custom-profile": {
				Arch:       "amd64",
				Platform:   "metal",
				KernelArgs: []string{"amd_iommu=off", "nomodeset"},
				Extensions: []string{"siderolabs/i915"},
			},
		},
		Nodes: []config.Node{},
	}

	err := runURLs("1.9.0")
	require.NoError(t, err)
}

func TestRunURLs_EmptyProfiles(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{},
		Nodes:    []config.Node{},
	}

	err := runURLs("1.9.0")
	require.NoError(t, err)
}

// ============================================================================
// runUpgradeWithClients() Tests
// ============================================================================

func TestRunUpgradeWithClients_Success(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleWorker},
		},
	}
	dryRun = true // Use dry run to simplify test
	defer func() { dryRun = false }()

	talosMock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			return talos.NodeStatus{IP: nodeIP, Version: "1.9.0", Profile: profile, Role: role}
		},
	}

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return fmt.Sprintf("factory.talos.dev/installer/abc:v%s", version), nil
		},
	}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "all", "1.9.0")
	require.NoError(t, err)
}

func TestRunUpgradeWithClients_NoNodesFound(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{}, // No nodes
	}
	dryRun = false

	talosMock := &talos.MockClient{}
	factoryMock := &factory.MockFactoryClient{}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "all", "1.9.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no nodes found")
}

func TestRunUpgradeWithClients_UnknownTarget(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleWorker},
		},
	}
	dryRun = false

	talosMock := &talos.MockClient{}
	factoryMock := &factory.MockFactoryClient{}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "unknown-target", "1.9.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown target")
}

func TestRunUpgradeWithClients_DryRun(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleWorker},
		},
	}
	dryRun = true
	defer func() { dryRun = false }()

	upgradeWasCalled := false
	talosMock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			upgradeWasCalled = true
			return nil
		},
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			return talos.NodeStatus{IP: nodeIP, Version: "1.9.0", Profile: profile, Role: role}
		},
	}

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return "factory.talos.dev/installer/abc:v1.9.0", nil
		},
	}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "all", "1.9.0")
	require.NoError(t, err)
	assert.False(t, upgradeWasCalled, "upgrade should not be called in dry run mode")
}

func TestRunUpgradeWithClients_UserCancels(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleWorker},
		},
	}
	dryRun = false

	// Mock user saying "no"
	origReader := confirmReader
	confirmReader = strings.NewReader("n\n")
	defer func() { confirmReader = origReader }()

	factoryCalled := false
	talosMock := &talos.MockClient{}
	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			factoryCalled = true
			return "factory.talos.dev/installer/abc:v1.9.0", nil
		},
	}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "all", "1.9.0")
	require.NoError(t, err) // Cancellation is not an error
	assert.False(t, factoryCalled, "factory should not be called after user cancels")
}

func TestRunUpgradeWithClients_FactoryError(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleWorker},
		},
	}
	dryRun = false

	// Mock user confirming
	origReader := confirmReader
	confirmReader = strings.NewReader("y\n")
	defer func() { confirmReader = origReader }()

	talosMock := &talos.MockClient{}
	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return "", fmt.Errorf("API error: rate limited")
		},
	}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "all", "1.9.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get image")
	assert.Contains(t, err.Error(), "test-profile")
}

func TestRunUpgradeWithClients_NodeFailure(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleWorker},
			{IP: "192.168.1.2", Profile: "test-profile", Role: config.RoleWorker},
		},
	}
	dryRun = false

	// Mock user confirming
	origReader := confirmReader
	confirmReader = strings.NewReader("y\n")
	defer func() { confirmReader = origReader }()

	nodesUpgraded := make(map[string]bool)
	talosMock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			if nodeIP == "192.168.1.1" {
				return fmt.Errorf("disk full")
			}
			nodesUpgraded[nodeIP] = true
			return nil
		},
		WatchUpgradeFunc: func(ctx context.Context, nodeIP string, timeout time.Duration, cb talos.ProgressCallback) error {
			return nil
		},
		WaitForServicesFunc: func(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error {
			return nil
		},
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			return talos.NodeStatus{IP: nodeIP, Version: "1.9.0", Profile: profile, Role: role}
		},
	}

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return "factory.talos.dev/installer/abc:v1.9.0", nil
		},
	}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "all", "1.9.0")
	require.NoError(t, err) // Overall should succeed even with one node failure
	assert.True(t, nodesUpgraded["192.168.1.2"], "second node should still be upgraded")
}

func TestRunUpgradeWithClients_CPFailureAbort(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleControlPlane},
			{IP: "192.168.1.2", Profile: "test-profile", Role: config.RoleWorker},
		},
	}
	dryRun = false

	// Mock user confirming first prompt, then aborting on CP failure
	origReader := confirmReader
	confirmReader = strings.NewReader("y\nn\n")
	defer func() { confirmReader = origReader }()

	secondNodeAttempted := false
	talosMock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			if nodeIP == "192.168.1.1" {
				return fmt.Errorf("etcd cluster unhealthy")
			}
			secondNodeAttempted = true
			return nil
		},
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			return talos.NodeStatus{IP: nodeIP, Version: "1.9.0", Profile: profile, Role: role}
		},
	}

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return "factory.talos.dev/installer/abc:v1.9.0", nil
		},
	}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "controlplanes", "1.9.0")
	require.NoError(t, err)
	assert.False(t, secondNodeAttempted, "should not attempt second node after user aborts")
}

func TestRunUpgradeWithClients_AlreadyAtVersion(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleWorker},
		},
	}
	dryRun = false

	// Mock user confirming
	origReader := confirmReader
	confirmReader = strings.NewReader("y\n")
	defer func() { confirmReader = origReader }()

	upgradeWasCalled := false
	talosMock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.9.0", nil // Already at target version
		},
		UpgradeFunc: func(ctx context.Context, nodeIP, image string, pres bool) error {
			upgradeWasCalled = true
			return nil
		},
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			return talos.NodeStatus{IP: nodeIP, Version: "1.9.0", Profile: profile, Role: role}
		},
	}

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return "factory.talos.dev/installer/abc:v1.9.0", nil
		},
	}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "all", "1.9.0")
	require.NoError(t, err)
	assert.False(t, upgradeWasCalled, "should skip upgrade for node already at version")
}

func TestRunUpgradeWithClients_MultipleProfiles(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"profile-a": {Arch: "amd64", Platform: "metal"},
			"profile-b": {Arch: "arm64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleWorker},
			{IP: "192.168.1.2", Profile: "profile-b", Role: config.RoleWorker},
		},
	}
	dryRun = true
	defer func() { dryRun = false }()

	profilesRequested := make(map[string]bool)
	talosMock := &talos.MockClient{
		GetVersionFunc: func(ctx context.Context, nodeIP string) (string, error) {
			return "1.8.0", nil
		},
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			return talos.NodeStatus{IP: nodeIP, Version: "1.9.0", Profile: profile, Role: role}
		},
	}

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			if profile.Arch == "amd64" {
				profilesRequested["profile-a"] = true
			} else if profile.Arch == "arm64" {
				profilesRequested["profile-b"] = true
			}
			return fmt.Sprintf("factory.talos.dev/installer/%s:v%s", profile.Arch, version), nil
		},
	}

	err := runUpgradeWithClients(context.Background(), talosMock, factoryMock, "all", "1.9.0")
	require.NoError(t, err)
	assert.True(t, profilesRequested["profile-a"], "should request image for profile-a")
	assert.True(t, profilesRequested["profile-b"], "should request image for profile-b")
}
