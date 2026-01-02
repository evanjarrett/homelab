package cmd

import (
	"fmt"
	"testing"

	"github.com/evanjarrett/homelab/internal/talosupgrade/config"
	"github.com/evanjarrett/homelab/internal/talosupgrade/factory"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// runImagesWithClient() Tests
// ============================================================================

func TestRunImagesWithClient_SingleProfile(t *testing.T) {
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

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return fmt.Sprintf("factory.talos.dev/installer/abc123:v%s", version), nil
		},
	}

	err := runImagesWithClient(factoryMock, "1.9.0")
	require.NoError(t, err)
}

func TestRunImagesWithClient_MultipleProfiles(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{
			"zebra-profile": {Arch: "amd64", Platform: "metal"},
			"alpha-profile": {Arch: "arm64", Platform: "metal"},
		},
		Nodes: []config.Node{},
	}

	callCount := 0
	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			callCount++
			return fmt.Sprintf("factory.talos.dev/installer/schematic%d:v%s", callCount, version), nil
		},
	}

	err := runImagesWithClient(factoryMock, "1.9.0")
	require.NoError(t, err)
	require.Equal(t, 2, callCount, "should call factory for each profile")
}

func TestRunImagesWithClient_FactoryError(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{},
	}

	factoryMock := &factory.MockFactoryClient{
		GetInstallerImageFunc: func(profile config.Profile, version string) (string, error) {
			return "", fmt.Errorf("factory API error")
		},
	}

	// runImagesWithClient continues on error (logs but doesn't fail)
	err := runImagesWithClient(factoryMock, "1.9.0")
	require.NoError(t, err)
}

func TestRunImagesWithClient_EmptyProfiles(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{},
		Nodes:    []config.Node{},
	}

	factoryMock := &factory.MockFactoryClient{}

	err := runImagesWithClient(factoryMock, "1.9.0")
	require.NoError(t, err)
}

func TestRunImagesWithClient_WithNodes(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{
			"test-profile": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "test-profile", Role: config.RoleControlPlane},
			{IP: "192.168.1.2", Profile: "test-profile", Role: config.RoleWorker},
			{IP: "192.168.1.3", Profile: "test-profile", Role: config.RoleWorker},
		},
	}

	factoryMock := &factory.MockFactoryClient{}

	err := runImagesWithClient(factoryMock, "1.9.0")
	require.NoError(t, err)
}
