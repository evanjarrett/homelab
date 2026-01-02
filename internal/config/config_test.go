package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testdataDir returns the path to the testdata directory
func testdataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(dir, "testdata")
}

func testdataPath(t *testing.T, filename string) string {
	t.Helper()
	return filepath.Join(testdataDir(t), filename)
}

// ============================================================================
// Load() Tests
// ============================================================================

func TestLoad_ValidConfig(t *testing.T) {
	cfg, err := Load(testdataPath(t, "valid-config.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Len(t, cfg.Profiles, 2)
	assert.Len(t, cfg.Nodes, 3)

	// Verify profile content
	intelProfile, ok := cfg.Profiles["amd64-intel"]
	require.True(t, ok)
	assert.Equal(t, "amd64", intelProfile.Arch)
	assert.Equal(t, "metal", intelProfile.Platform)
	assert.True(t, intelProfile.Secureboot)
	assert.Contains(t, intelProfile.Extensions, "siderolabs/i915")

	// Verify node content
	assert.Equal(t, "192.168.1.101", cfg.Nodes[0].IP)
	assert.Equal(t, "arm64-rpi", cfg.Nodes[0].Profile)
	assert.Equal(t, "worker", cfg.Nodes[0].Role)

	// Verify settings
	assert.Equal(t, "https://factory.talos.dev", cfg.Settings.FactoryBaseURL)
	assert.Equal(t, 600, cfg.Settings.DefaultTimeoutSeconds)
}

func TestLoad_MinimalConfig(t *testing.T) {
	cfg, err := Load(testdataPath(t, "minimal-config.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Len(t, cfg.Profiles, 1)
	assert.Len(t, cfg.Nodes, 1)
}

func TestLoad_EmptyPathNoDefaultConfig(t *testing.T) {
	// Change to temp dir with no config files to test findDefaultConfig() failure
	origDir, err := os.Getwd()
	require.NoError(t, err)

	tmpDir := t.TempDir()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer os.Chdir(origDir)

	cfg, err := Load("")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "no config file found")
}

func TestLoad_NonexistentFile(t *testing.T) {
	cfg, err := Load(testdataPath(t, "nonexistent.yaml"))
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoad_MalformedYAML(t *testing.T) {
	cfg, err := Load(testdataPath(t, "malformed.yaml"))
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

func TestLoad_InvalidNoNodes(t *testing.T) {
	cfg, err := Load(testdataPath(t, "invalid-no-nodes.yaml"))
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "no nodes defined")
}

func TestLoad_InvalidNoProfiles(t *testing.T) {
	cfg, err := Load(testdataPath(t, "invalid-no-profiles.yaml"))
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "no profiles defined")
}

func TestLoad_InvalidUnknownProfile(t *testing.T) {
	cfg, err := Load(testdataPath(t, "invalid-unknown-profile.yaml"))
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "references unknown profile")
}

func TestLoad_InvalidMissingArch(t *testing.T) {
	cfg, err := Load(testdataPath(t, "invalid-missing-arch.yaml"))
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "arch is required")
}

func TestLoad_InvalidBadRole(t *testing.T) {
	cfg, err := Load(testdataPath(t, "invalid-bad-role.yaml"))
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "role must be 'controlplane' or 'worker'")
}

// ============================================================================
// Validate() Tests
// ============================================================================

func TestValidate_Success(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"test": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "test", Role: RoleControlPlane},
		},
	}
	assert.NoError(t, cfg.Validate())
}

func TestValidate_EmptyProfiles(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{},
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "test", Role: RoleControlPlane},
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no profiles defined")
}

func TestValidate_EmptyNodes(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"test": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []Node{},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no nodes defined")
}

func TestValidate_ProfileMissingArch(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"test": {Platform: "metal"},
		},
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "test", Role: RoleControlPlane},
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "arch is required")
}

func TestValidate_ProfileMissingPlatform(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"test": {Arch: "amd64"},
		},
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "test", Role: RoleControlPlane},
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "platform is required")
}

func TestValidate_NodeEmptyIP(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"test": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []Node{
			{IP: "", Profile: "test", Role: RoleControlPlane},
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "node with empty IP")
}

func TestValidate_NodeEmptyProfile(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"test": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "", Role: RoleControlPlane},
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "profile is required")
}

func TestValidate_NodeUnknownProfile(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"test": {Arch: "amd64", Platform: "metal"},
		},
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "unknown", Role: RoleControlPlane},
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown profile")
}

func TestValidate_NodeInvalidRole(t *testing.T) {
	tests := []struct {
		name string
		role string
	}{
		{"empty role", ""},
		{"master role", "master"},
		{"invalid role", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Profiles: map[string]Profile{
					"test": {Arch: "amd64", Platform: "metal"},
				},
				Nodes: []Node{
					{IP: "192.168.1.1", Profile: "test", Role: tt.role},
				},
			}
			err := cfg.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "role must be 'controlplane' or 'worker'")
		})
	}
}

// ============================================================================
// SetDefaults() Tests
// ============================================================================

func TestSetDefaults_Empty(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	assert.Equal(t, "https://factory.talos.dev", cfg.Settings.FactoryBaseURL)
	assert.Equal(t, 600, cfg.Settings.DefaultTimeoutSeconds)
	assert.Equal(t, "https://api.github.com/repos/siderolabs/talos/releases/latest", cfg.Settings.GithubReleasesURL)
}

func TestSetDefaults_PreservesExisting(t *testing.T) {
	cfg := &Config{
		Settings: Settings{
			FactoryBaseURL:        "https://custom.factory.dev",
			DefaultTimeoutSeconds: 1200,
			GithubReleasesURL:     "https://custom.github.api/releases",
		},
	}
	cfg.SetDefaults()

	assert.Equal(t, "https://custom.factory.dev", cfg.Settings.FactoryBaseURL)
	assert.Equal(t, 1200, cfg.Settings.DefaultTimeoutSeconds)
	assert.Equal(t, "https://custom.github.api/releases", cfg.Settings.GithubReleasesURL)
}

func TestSetDefaults_PartialSettings(t *testing.T) {
	cfg := &Config{
		Settings: Settings{
			FactoryBaseURL: "https://custom.factory.dev",
			// DefaultTimeoutSeconds and GithubReleasesURL are zero/empty
		},
	}
	cfg.SetDefaults()

	assert.Equal(t, "https://custom.factory.dev", cfg.Settings.FactoryBaseURL)
	assert.Equal(t, 600, cfg.Settings.DefaultTimeoutSeconds)
	assert.Equal(t, "https://api.github.com/repos/siderolabs/talos/releases/latest", cfg.Settings.GithubReleasesURL)
}

// ============================================================================
// GetNodesByRole() Tests
// ============================================================================

func TestGetNodesByRole(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{IP: "192.168.1.1", Role: RoleControlPlane},
			{IP: "192.168.1.2", Role: RoleWorker},
			{IP: "192.168.1.3", Role: RoleWorker},
			{IP: "192.168.1.4", Role: RoleControlPlane},
		},
	}

	t.Run("control planes", func(t *testing.T) {
		nodes := cfg.GetNodesByRole(RoleControlPlane)
		assert.Len(t, nodes, 2)
		assert.Equal(t, "192.168.1.1", nodes[0].IP)
		assert.Equal(t, "192.168.1.4", nodes[1].IP)
	})

	t.Run("workers", func(t *testing.T) {
		nodes := cfg.GetNodesByRole(RoleWorker)
		assert.Len(t, nodes, 2)
		assert.Equal(t, "192.168.1.2", nodes[0].IP)
		assert.Equal(t, "192.168.1.3", nodes[1].IP)
	})

	t.Run("no matching role", func(t *testing.T) {
		nodes := cfg.GetNodesByRole("unknown")
		assert.Empty(t, nodes)
	})
}

// ============================================================================
// GetNodesByProfile() Tests
// ============================================================================

func TestGetNodesByProfile(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "profile-a"},
			{IP: "192.168.1.2", Profile: "profile-b"},
			{IP: "192.168.1.3", Profile: "profile-a"},
			{IP: "192.168.1.4", Profile: "profile-c"},
		},
	}

	t.Run("profile-a", func(t *testing.T) {
		nodes := cfg.GetNodesByProfile("profile-a")
		assert.Len(t, nodes, 2)
		assert.Equal(t, "192.168.1.1", nodes[0].IP)
		assert.Equal(t, "192.168.1.3", nodes[1].IP)
	})

	t.Run("profile-b", func(t *testing.T) {
		nodes := cfg.GetNodesByProfile("profile-b")
		assert.Len(t, nodes, 1)
		assert.Equal(t, "192.168.1.2", nodes[0].IP)
	})

	t.Run("no matching profile", func(t *testing.T) {
		nodes := cfg.GetNodesByProfile("unknown")
		assert.Empty(t, nodes)
	})
}

// ============================================================================
// GetNodeByIP() Tests
// ============================================================================

func TestGetNodeByIP(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "test", Role: RoleControlPlane},
			{IP: "192.168.1.2", Profile: "test", Role: RoleWorker},
		},
	}

	t.Run("existing node", func(t *testing.T) {
		node := cfg.GetNodeByIP("192.168.1.1")
		require.NotNil(t, node)
		assert.Equal(t, "192.168.1.1", node.IP)
		assert.Equal(t, RoleControlPlane, node.Role)
	})

	t.Run("non-existing node", func(t *testing.T) {
		node := cfg.GetNodeByIP("192.168.1.99")
		assert.Nil(t, node)
	})

	t.Run("empty IP", func(t *testing.T) {
		node := cfg.GetNodeByIP("")
		assert.Nil(t, node)
	})
}

// ============================================================================
// GetProfileForNode() Tests
// ============================================================================

func TestGetProfileForNode(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"profile-a": {Arch: "amd64", Platform: "metal"},
			"profile-b": {Arch: "arm64", Platform: "metal"},
		},
		Nodes: []Node{
			{IP: "192.168.1.1", Profile: "profile-a"},
			{IP: "192.168.1.2", Profile: "profile-b"},
		},
	}

	t.Run("existing node with profile", func(t *testing.T) {
		profile := cfg.GetProfileForNode("192.168.1.1")
		require.NotNil(t, profile)
		assert.Equal(t, "amd64", profile.Arch)
	})

	t.Run("non-existing node", func(t *testing.T) {
		profile := cfg.GetProfileForNode("192.168.1.99")
		assert.Nil(t, profile)
	})

	t.Run("node with missing profile definition", func(t *testing.T) {
		cfg := &Config{
			Profiles: map[string]Profile{
				"profile-a": {Arch: "amd64", Platform: "metal"},
			},
			Nodes: []Node{
				{IP: "192.168.1.1", Profile: "missing-profile"},
			},
		}
		profile := cfg.GetProfileForNode("192.168.1.1")
		assert.Nil(t, profile)
	})
}

// ============================================================================
// GetControlPlaneNodes() and GetWorkerNodes() Tests
// ============================================================================

func TestGetControlPlaneNodes(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{IP: "192.168.1.1", Role: RoleControlPlane},
			{IP: "192.168.1.2", Role: RoleWorker},
			{IP: "192.168.1.3", Role: RoleControlPlane},
		},
	}

	nodes := cfg.GetControlPlaneNodes()
	assert.Len(t, nodes, 2)
	assert.Equal(t, "192.168.1.1", nodes[0].IP)
	assert.Equal(t, "192.168.1.3", nodes[1].IP)
}

func TestGetWorkerNodes(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{IP: "192.168.1.1", Role: RoleControlPlane},
			{IP: "192.168.1.2", Role: RoleWorker},
			{IP: "192.168.1.3", Role: RoleWorker},
		},
	}

	nodes := cfg.GetWorkerNodes()
	assert.Len(t, nodes, 2)
	assert.Equal(t, "192.168.1.2", nodes[0].IP)
	assert.Equal(t, "192.168.1.3", nodes[1].IP)
}

// ============================================================================
// GetAllNodesOrdered() Tests
// ============================================================================

func TestGetAllNodesOrdered(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{IP: "192.168.1.1", Role: RoleControlPlane},
			{IP: "192.168.1.2", Role: RoleWorker},
			{IP: "192.168.1.3", Role: RoleControlPlane},
			{IP: "192.168.1.4", Role: RoleWorker},
		},
	}

	nodes := cfg.GetAllNodesOrdered()
	require.Len(t, nodes, 4)

	// Workers should come first
	assert.Equal(t, "192.168.1.2", nodes[0].IP)
	assert.Equal(t, RoleWorker, nodes[0].Role)
	assert.Equal(t, "192.168.1.4", nodes[1].IP)
	assert.Equal(t, RoleWorker, nodes[1].Role)

	// Then control planes
	assert.Equal(t, "192.168.1.1", nodes[2].IP)
	assert.Equal(t, RoleControlPlane, nodes[2].Role)
	assert.Equal(t, "192.168.1.3", nodes[3].IP)
	assert.Equal(t, RoleControlPlane, nodes[3].Role)
}

func TestGetAllNodesOrdered_OnlyWorkers(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{IP: "192.168.1.1", Role: RoleWorker},
			{IP: "192.168.1.2", Role: RoleWorker},
		},
	}

	nodes := cfg.GetAllNodesOrdered()
	assert.Len(t, nodes, 2)
}

func TestGetAllNodesOrdered_OnlyControlPlanes(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{IP: "192.168.1.1", Role: RoleControlPlane},
			{IP: "192.168.1.2", Role: RoleControlPlane},
		},
	}

	nodes := cfg.GetAllNodesOrdered()
	assert.Len(t, nodes, 2)
}

// ============================================================================
// GetLatestTalosVersion() Tests
// ============================================================================

func TestGetLatestTalosVersion_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]string{"tag_name": "v1.7.0"}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	version, err := GetLatestTalosVersion(server.URL)
	require.NoError(t, err)
	assert.Equal(t, "1.7.0", version) // 'v' should be stripped
}

func TestGetLatestTalosVersion_WithoutVPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]string{"tag_name": "1.7.0"}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	version, err := GetLatestTalosVersion(server.URL)
	require.NoError(t, err)
	assert.Equal(t, "1.7.0", version)
}

func TestGetLatestTalosVersion_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	version, err := GetLatestTalosVersion(server.URL)
	assert.Error(t, err)
	assert.Empty(t, version)
	assert.Contains(t, err.Error(), "returned status 500")
}

func TestGetLatestTalosVersion_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	version, err := GetLatestTalosVersion(server.URL)
	assert.Error(t, err)
	assert.Empty(t, version)
	assert.Contains(t, err.Error(), "returned status 404")
}

func TestGetLatestTalosVersion_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	version, err := GetLatestTalosVersion(server.URL)
	assert.Error(t, err)
	assert.Empty(t, version)
	assert.Contains(t, err.Error(), "failed to decode")
}

func TestGetLatestTalosVersion_EmptyVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]string{"tag_name": ""}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	version, err := GetLatestTalosVersion(server.URL)
	assert.Error(t, err)
	assert.Empty(t, version)
	assert.Contains(t, err.Error(), "empty version")
}

func TestGetLatestTalosVersion_ConnectionError(t *testing.T) {
	// Use an invalid URL that will fail to connect
	version, err := GetLatestTalosVersion("http://localhost:59999")
	assert.Error(t, err)
	assert.Empty(t, version)
	assert.Contains(t, err.Error(), "failed to fetch")
}

func TestGetLatestTalosVersion_DefaultURL(t *testing.T) {
	// This just ensures we don't panic when empty URL is passed
	// and the default is used. We expect an error since we're not mocking GitHub.
	version, err := GetLatestTalosVersion("")
	// We expect either an error (network issues) or success (if GitHub is reachable)
	// The important thing is it doesn't panic
	_ = version
	_ = err
}
