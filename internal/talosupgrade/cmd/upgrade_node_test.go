package cmd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/evanjarrett/homelab/internal/talosupgrade/config"
	"github.com/evanjarrett/homelab/internal/talosupgrade/factory"
	"github.com/evanjarrett/homelab/internal/talosupgrade/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// runUpgradeNodeWithClients() Tests
// ============================================================================

func TestRunUpgradeNodeWithClients_Success(t *testing.T) {
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
	preserve = true

	talosMock := &talos.MockClient{
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
			return nil
		},
	}

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return fmt.Sprintf("factory.talos.dev/installer/test:v%s", version), nil
		},
	}

	err := runUpgradeNodeWithClients(context.Background(), talosMock, factoryMock, "192.168.1.1", "1.9.0")
	require.NoError(t, err)
}

func TestRunUpgradeNodeWithClients_UnknownNode(t *testing.T) {
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

	talosMock := &talos.MockClient{}
	factoryMock := &factory.MockFactoryClient{}

	err := runUpgradeNodeWithClients(context.Background(), talosMock, factoryMock, "192.168.1.99", "1.9.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node")
}

func TestRunUpgradeNodeWithClients_UnknownProfile(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 60,
		},
		Profiles: map[string]config.Profile{
			"other-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "missing-profile", Role: config.RoleWorker},
		},
	}

	talosMock := &talos.MockClient{}
	factoryMock := &factory.MockFactoryClient{}

	err := runUpgradeNodeWithClients(context.Background(), talosMock, factoryMock, "192.168.1.1", "1.9.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown profile")
}

func TestRunUpgradeNodeWithClients_FactoryError(t *testing.T) {
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

	talosMock := &talos.MockClient{}
	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return "", fmt.Errorf("factory API error: connection refused")
		},
	}

	err := runUpgradeNodeWithClients(context.Background(), talosMock, factoryMock, "192.168.1.1", "1.9.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get image")
}

func TestRunUpgradeNodeWithClients_DryRun(t *testing.T) {
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
	preserve = true
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
	}

	factoryMock := &factory.MockFactoryClient{}

	err := runUpgradeNodeWithClients(context.Background(), talosMock, factoryMock, "192.168.1.1", "1.9.0")
	require.NoError(t, err)
	assert.False(t, upgradeWasCalled, "upgrade should not be called in dry run mode")
}
