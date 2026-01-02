package factory

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/evanjarrett/homelab/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ============================================================================
// NewClient() Tests
// ============================================================================

func TestNewClient_DefaultURL(t *testing.T) {
	client := NewClient("")
	assert.Equal(t, "https://factory.talos.dev", client.baseURL)
	assert.NotNil(t, client.httpClient)
}

func TestNewClient_CustomURL(t *testing.T) {
	client := NewClient("https://custom.factory.dev")
	assert.Equal(t, "https://custom.factory.dev", client.baseURL)
}

// ============================================================================
// BuildSchematic() Tests
// ============================================================================

func TestBuildSchematic_MinimalProfile(t *testing.T) {
	profile := config.Profile{
		Arch:       "amd64",
		Platform:   "metal",
		Extensions: []string{},
	}

	schematic := BuildSchematic(profile)
	require.NotNil(t, schematic)
	assert.NotNil(t, schematic.Customization)
	assert.Nil(t, schematic.Customization.SystemExtensions)
	assert.Empty(t, schematic.Customization.ExtraKernelArgs)
	assert.Nil(t, schematic.Overlay)
}

func TestBuildSchematic_WithExtensions(t *testing.T) {
	profile := config.Profile{
		Arch:     "amd64",
		Platform: "metal",
		Extensions: []string{
			"siderolabs/i915",
			"siderolabs/iscsi-tools",
		},
	}

	schematic := BuildSchematic(profile)
	require.NotNil(t, schematic)
	require.NotNil(t, schematic.Customization)
	require.NotNil(t, schematic.Customization.SystemExtensions)
	assert.Len(t, schematic.Customization.SystemExtensions.OfficialExtensions, 2)
	assert.Contains(t, schematic.Customization.SystemExtensions.OfficialExtensions, "siderolabs/i915")
	assert.Contains(t, schematic.Customization.SystemExtensions.OfficialExtensions, "siderolabs/iscsi-tools")
}

func TestBuildSchematic_WithKernelArgs(t *testing.T) {
	profile := config.Profile{
		Arch:     "amd64",
		Platform: "metal",
		KernelArgs: []string{
			"amd_iommu=off",
			"nomodeset",
		},
	}

	schematic := BuildSchematic(profile)
	require.NotNil(t, schematic)
	assert.Len(t, schematic.Customization.ExtraKernelArgs, 2)
	assert.Contains(t, schematic.Customization.ExtraKernelArgs, "amd_iommu=off")
	assert.Contains(t, schematic.Customization.ExtraKernelArgs, "nomodeset")
}

func TestBuildSchematic_WithOverlay(t *testing.T) {
	profile := config.Profile{
		Arch:     "arm64",
		Platform: "metal",
		Overlay: &config.Overlay{
			Name:  "rpi_generic",
			Image: "siderolabs/sbc-raspberrypi",
		},
	}

	schematic := BuildSchematic(profile)
	require.NotNil(t, schematic)
	require.NotNil(t, schematic.Overlay)
	assert.Equal(t, "rpi_generic", schematic.Overlay.Name)
	assert.Equal(t, "siderolabs/sbc-raspberrypi", schematic.Overlay.Image)
}

func TestBuildSchematic_CompleteProfile(t *testing.T) {
	profile := config.Profile{
		Arch:       "arm64",
		Platform:   "metal",
		Secureboot: false,
		KernelArgs: []string{"console=ttyS0"},
		Extensions: []string{"siderolabs/iscsi-tools"},
		Overlay: &config.Overlay{
			Name:  "turingrk1",
			Image: "siderolabs/sbc-rockchip",
		},
	}

	schematic := BuildSchematic(profile)
	require.NotNil(t, schematic)
	require.NotNil(t, schematic.Customization)
	require.NotNil(t, schematic.Customization.SystemExtensions)
	require.NotNil(t, schematic.Overlay)

	assert.Len(t, schematic.Customization.ExtraKernelArgs, 1)
	assert.Len(t, schematic.Customization.SystemExtensions.OfficialExtensions, 1)
	assert.Equal(t, "turingrk1", schematic.Overlay.Name)
}

func TestBuildSchematic_YAMLOutput(t *testing.T) {
	profile := config.Profile{
		Arch:     "amd64",
		Platform: "metal",
		Extensions: []string{
			"siderolabs/i915",
		},
	}

	schematic := BuildSchematic(profile)
	yamlBytes, err := yaml.Marshal(schematic)
	require.NoError(t, err)

	// Verify YAML can be unmarshalled back
	var parsed Schematic
	err = yaml.Unmarshal(yamlBytes, &parsed)
	require.NoError(t, err)
	require.NotNil(t, parsed.Customization.SystemExtensions)
	assert.Contains(t, parsed.Customization.SystemExtensions.OfficialExtensions, "siderolabs/i915")
}

// ============================================================================
// GetSchematicID() Tests
// ============================================================================

func TestGetSchematicID_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/schematics", r.URL.Path)
		assert.Equal(t, "application/yaml", r.Header.Get("Content-Type"))

		// Read and verify body is valid YAML
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var schematic Schematic
		err = yaml.Unmarshal(body, &schematic)
		assert.NoError(t, err)

		// Return success
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(SchematicResponse{ID: "abc123def456"})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	schematic := &Schematic{
		Customization: &SchematicCustomization{
			SystemExtensions: &SchematicSystemExtensions{
				OfficialExtensions: []string{"siderolabs/i915"},
			},
		},
	}

	id, err := client.GetSchematicID(schematic)
	require.NoError(t, err)
	assert.Equal(t, "abc123def456", id)
}

func TestGetSchematicID_Created201(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(SchematicResponse{ID: "newschematic123"})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	id, err := client.GetSchematicID(&Schematic{})
	require.NoError(t, err)
	assert.Equal(t, "newschematic123", id)
}

func TestGetSchematicID_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	id, err := client.GetSchematicID(&Schematic{})
	assert.Error(t, err)
	assert.Empty(t, id)
	assert.Contains(t, err.Error(), "returned status 500")
}

func TestGetSchematicID_BadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	id, err := client.GetSchematicID(&Schematic{})
	assert.Error(t, err)
	assert.Empty(t, id)
	assert.Contains(t, err.Error(), "returned status 400")
}

func TestGetSchematicID_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	id, err := client.GetSchematicID(&Schematic{})
	assert.Error(t, err)
	assert.Empty(t, id)
	assert.Contains(t, err.Error(), "failed to decode")
}

func TestGetSchematicID_EmptyID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(SchematicResponse{ID: ""})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	id, err := client.GetSchematicID(&Schematic{})
	assert.Error(t, err)
	assert.Empty(t, id)
	assert.Contains(t, err.Error(), "empty schematic ID")
}

func TestGetSchematicID_ConnectionError(t *testing.T) {
	client := NewClient("http://localhost:59999")
	id, err := client.GetSchematicID(&Schematic{})
	assert.Error(t, err)
	assert.Empty(t, id)
	assert.Contains(t, err.Error(), "failed to post schematic")
}

// ============================================================================
// GetInstallerImage() Tests
// ============================================================================

func TestGetInstallerImage_Standard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(SchematicResponse{ID: "schematic123"})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	profile := config.Profile{
		Arch:       "amd64",
		Platform:   "metal",
		Secureboot: false,
		Extensions: []string{"siderolabs/i915"},
	}

	image, err := client.GetInstallerImage(profile, "1.7.0")
	require.NoError(t, err)
	assert.Equal(t, "factory.talos.dev/installer/schematic123:v1.7.0", image)
}

func TestGetInstallerImage_Secureboot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(SchematicResponse{ID: "secureboot456"})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	profile := config.Profile{
		Arch:       "amd64",
		Platform:   "metal",
		Secureboot: true,
		Extensions: []string{"siderolabs/i915"},
	}

	image, err := client.GetInstallerImage(profile, "1.7.0")
	require.NoError(t, err)
	assert.Equal(t, "factory.talos.dev/installer-secureboot/secureboot456:v1.7.0", image)
}

func TestGetInstallerImage_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	profile := config.Profile{
		Arch:     "amd64",
		Platform: "metal",
	}

	image, err := client.GetInstallerImage(profile, "1.7.0")
	assert.Error(t, err)
	assert.Empty(t, image)
}

// ============================================================================
// GenerateFactoryURL() Tests
// ============================================================================

func TestGenerateFactoryURL_BasicProfile(t *testing.T) {
	profile := config.Profile{
		Arch:       "amd64",
		Platform:   "metal",
		Secureboot: false,
	}

	urlStr := GenerateFactoryURL(profile, "1.7.0", "")

	parsed, err := url.Parse(urlStr)
	require.NoError(t, err)

	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "factory.talos.dev", parsed.Host)
	assert.Equal(t, "/", parsed.Path)

	params := parsed.Query()
	assert.Equal(t, "amd64", params.Get("arch"))
	assert.Equal(t, "metal", params.Get("platform"))
	assert.Equal(t, "metal", params.Get("target"))
	assert.Equal(t, "1.7.0", params.Get("version"))
	assert.Equal(t, "auto", params.Get("bootloader"))
	assert.Equal(t, "true", params.Get("cmdline-set"))
	assert.Empty(t, params.Get("secureboot"))
}

func TestGenerateFactoryURL_Secureboot(t *testing.T) {
	profile := config.Profile{
		Arch:       "amd64",
		Platform:   "metal",
		Secureboot: true,
	}

	urlStr := GenerateFactoryURL(profile, "1.7.0", "")

	parsed, err := url.Parse(urlStr)
	require.NoError(t, err)

	params := parsed.Query()
	assert.Equal(t, "true", params.Get("secureboot"))
	assert.Equal(t, "metal", params.Get("target"))
}

func TestGenerateFactoryURL_WithExtensions(t *testing.T) {
	profile := config.Profile{
		Arch:     "amd64",
		Platform: "metal",
		Extensions: []string{
			"siderolabs/i915",
			"siderolabs/iscsi-tools",
		},
	}

	urlStr := GenerateFactoryURL(profile, "1.7.0", "")

	parsed, err := url.Parse(urlStr)
	require.NoError(t, err)

	params := parsed.Query()
	extensions := params["extensions"]
	assert.Len(t, extensions, 2)
	assert.Contains(t, extensions, "siderolabs/i915")
	assert.Contains(t, extensions, "siderolabs/iscsi-tools")
}

func TestGenerateFactoryURL_WithKernelArgs(t *testing.T) {
	profile := config.Profile{
		Arch:     "amd64",
		Platform: "metal",
		KernelArgs: []string{
			"amd_iommu=off",
			"nomodeset",
		},
	}

	urlStr := GenerateFactoryURL(profile, "1.7.0", "")

	parsed, err := url.Parse(urlStr)
	require.NoError(t, err)

	params := parsed.Query()
	cmdline := params["cmdline"]
	assert.Len(t, cmdline, 2)
	assert.Contains(t, cmdline, "amd_iommu=off")
	assert.Contains(t, cmdline, "nomodeset")
}

func TestGenerateFactoryURL_SBCWithOverlay(t *testing.T) {
	profile := config.Profile{
		Arch:     "arm64",
		Platform: "metal",
		Overlay: &config.Overlay{
			Name:  "rpi_generic",
			Image: "siderolabs/sbc-raspberrypi",
		},
		Extensions: []string{"siderolabs/iscsi-tools"},
	}

	urlStr := GenerateFactoryURL(profile, "1.7.0", "")

	parsed, err := url.Parse(urlStr)
	require.NoError(t, err)

	params := parsed.Query()
	assert.Equal(t, "arm64", params.Get("arch"))
	assert.Equal(t, "sbc", params.Get("target"))
	assert.Equal(t, "rpi_generic", params.Get("board"))
	assert.Empty(t, params.Get("secureboot")) // SBCs don't use secureboot param

	// SBCs should have "-" to reset defaults, then extensions
	extensions := params["extensions"]
	assert.Contains(t, extensions, "-")
	assert.Contains(t, extensions, "siderolabs/iscsi-tools")
}

func TestGenerateFactoryURL_CustomBaseURL(t *testing.T) {
	profile := config.Profile{
		Arch:     "amd64",
		Platform: "metal",
	}

	urlStr := GenerateFactoryURL(profile, "1.7.0", "https://custom.factory.dev")

	parsed, err := url.Parse(urlStr)
	require.NoError(t, err)

	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "custom.factory.dev", parsed.Host)
}

func TestGenerateFactoryURL_CompleteProfile(t *testing.T) {
	profile := config.Profile{
		Arch:       "amd64",
		Platform:   "metal",
		Secureboot: true,
		KernelArgs: []string{"console=ttyS0"},
		Extensions: []string{
			"siderolabs/i915",
			"siderolabs/nut-client",
		},
	}

	urlStr := GenerateFactoryURL(profile, "1.8.0", "")

	parsed, err := url.Parse(urlStr)
	require.NoError(t, err)

	params := parsed.Query()
	assert.Equal(t, "amd64", params.Get("arch"))
	assert.Equal(t, "metal", params.Get("platform"))
	assert.Equal(t, "metal", params.Get("target"))
	assert.Equal(t, "true", params.Get("secureboot"))
	assert.Equal(t, "1.8.0", params.Get("version"))

	cmdline := params["cmdline"]
	assert.Contains(t, cmdline, "console=ttyS0")

	extensions := params["extensions"]
	assert.Contains(t, extensions, "siderolabs/i915")
	assert.Contains(t, extensions, "siderolabs/nut-client")
}
