package cmd

import (
	"fmt"
	"testing"

	"github.com/evanjarrett/homelab/internal/talosupgrade/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// getVersionWithResolver() Tests
// ============================================================================

func TestGetVersionWithResolver_FlagSet(t *testing.T) {
	// Save and restore global state
	originalVersion := talosVersion
	defer func() { talosVersion = originalVersion }()

	talosVersion = "1.8.0"

	// Resolver should not be called when flag is set
	resolverCalled := false
	mockResolver := func(url string) (string, error) {
		resolverCalled = true
		return "1.9.0", nil
	}

	version, err := getVersionWithResolver(mockResolver)
	require.NoError(t, err)
	assert.Equal(t, "1.8.0", version)
	assert.False(t, resolverCalled, "resolver should not be called when flag is set")
}

func TestGetVersionWithResolver_APISuccess(t *testing.T) {
	// Save and restore global state
	originalVersion := talosVersion
	originalCfg := cfg
	defer func() {
		talosVersion = originalVersion
		cfg = originalCfg
	}()

	talosVersion = "" // No flag set
	cfg = &config.Config{
		Settings: config.Settings{
			GithubReleasesURL: "https://api.github.com/repos/siderolabs/talos/releases/latest",
		},
	}

	mockResolver := func(url string) (string, error) {
		assert.Equal(t, "https://api.github.com/repos/siderolabs/talos/releases/latest", url)
		return "1.9.1", nil
	}

	version, err := getVersionWithResolver(mockResolver)
	require.NoError(t, err)
	assert.Equal(t, "1.9.1", version)
}

func TestGetVersionWithResolver_APIError_ReturnsFallback(t *testing.T) {
	// Save and restore global state
	originalVersion := talosVersion
	originalCfg := cfg
	defer func() {
		talosVersion = originalVersion
		cfg = originalCfg
	}()

	talosVersion = "" // No flag set
	cfg = &config.Config{
		Settings: config.Settings{
			GithubReleasesURL: "https://api.github.com/repos/siderolabs/talos/releases/latest",
		},
	}

	mockResolver := func(url string) (string, error) {
		return "", fmt.Errorf("network error: connection refused")
	}

	version, err := getVersionWithResolver(mockResolver)
	require.NoError(t, err) // Should not return error, uses fallback
	assert.Equal(t, "1.9.5", version)
}

func TestGetVersionWithResolver_EmptyURLConfig(t *testing.T) {
	// Save and restore global state
	originalVersion := talosVersion
	originalCfg := cfg
	defer func() {
		talosVersion = originalVersion
		cfg = originalCfg
	}()

	talosVersion = "" // No flag set
	cfg = &config.Config{
		Settings: config.Settings{
			GithubReleasesURL: "", // Empty URL
		},
	}

	mockResolver := func(url string) (string, error) {
		assert.Equal(t, "", url) // Should pass empty URL
		return "1.9.2", nil
	}

	version, err := getVersionWithResolver(mockResolver)
	require.NoError(t, err)
	assert.Equal(t, "1.9.2", version)
}

// ============================================================================
// loadConfigWithLoader() Tests
// ============================================================================

func TestLoadConfigWithLoader_Success(t *testing.T) {
	// Save and restore global state
	originalCfgFile := cfgFile
	originalCfg := cfg
	defer func() {
		cfgFile = originalCfgFile
		cfg = originalCfg
	}()

	cfgFile = "test-config.yaml"
	cfg = nil

	expectedConfig := &config.Config{
		Settings: config.Settings{
			FactoryBaseURL: "https://factory.talos.dev",
		},
		Profiles: map[string]config.Profile{
			"test": {Arch: "amd64", Platform: "metal"},
		},
	}

	mockLoader := func(path string) (*config.Config, error) {
		assert.Equal(t, "test-config.yaml", path)
		return expectedConfig, nil
	}

	err := loadConfigWithLoader(mockLoader)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "https://factory.talos.dev", cfg.Settings.FactoryBaseURL)
}

func TestLoadConfigWithLoader_Error(t *testing.T) {
	// Save and restore global state
	originalCfgFile := cfgFile
	originalCfg := cfg
	defer func() {
		cfgFile = originalCfgFile
		cfg = originalCfg
	}()

	cfgFile = "nonexistent.yaml"
	cfg = nil

	mockLoader := func(path string) (*config.Config, error) {
		return nil, fmt.Errorf("config file not found: %s", path)
	}

	err := loadConfigWithLoader(mockLoader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file not found")
	assert.Nil(t, cfg)
}

func TestLoadConfigWithLoader_DefaultsApplied(t *testing.T) {
	// Save and restore global state
	originalCfgFile := cfgFile
	originalCfg := cfg
	defer func() {
		cfgFile = originalCfgFile
		cfg = originalCfg
	}()

	cfgFile = "test-config.yaml"
	cfg = nil

	// Return a config with empty settings to verify SetDefaults is called
	mockLoader := func(path string) (*config.Config, error) {
		return &config.Config{
			Settings: config.Settings{
				// Empty - SetDefaults should fill these
			},
			Profiles: map[string]config.Profile{},
		}, nil
	}

	err := loadConfigWithLoader(mockLoader)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	// SetDefaults should have been called
	assert.NotEmpty(t, cfg.Settings.FactoryBaseURL, "SetDefaults should set FactoryBaseURL")
}
