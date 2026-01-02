package factory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/evanjarrett/homelab/internal/talosupgrade/config"
	"gopkg.in/yaml.v3"
)

// Schematic represents the YAML schema sent to factory.talos.dev
type Schematic struct {
	Overlay       *SchematicOverlay       `yaml:"overlay,omitempty"`
	Customization *SchematicCustomization `yaml:"customization,omitempty"`
}

// SchematicOverlay for SBC boards
type SchematicOverlay struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

// SchematicCustomization for extensions and kernel args
type SchematicCustomization struct {
	ExtraKernelArgs  []string                   `yaml:"extraKernelArgs,omitempty"`
	SystemExtensions *SchematicSystemExtensions `yaml:"systemExtensions,omitempty"`
}

// SchematicSystemExtensions lists official extensions
type SchematicSystemExtensions struct {
	OfficialExtensions []string `yaml:"officialExtensions"`
}

// SchematicResponse from factory.talos.dev/schematics POST
type SchematicResponse struct {
	ID string `json:"id"`
}

// FactoryClientInterface defines the interface for factory API operations.
// This enables mocking the factory client for testing.
type FactoryClientInterface interface {
	GetInstallerImage(profile config.Profile, version string) (string, error)
	GetSchematicID(schematic *Schematic) (string, error)
}

// Client for interacting with the Talos Factory API
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Ensure Client implements FactoryClientInterface
var _ FactoryClientInterface = (*Client)(nil)

// NewClient creates a new Factory API client
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://factory.talos.dev"
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// BuildSchematic creates a Schematic from a Profile
func BuildSchematic(profile config.Profile) *Schematic {
	schematic := &Schematic{
		Customization: &SchematicCustomization{},
	}

	// Add extensions
	if len(profile.Extensions) > 0 {
		schematic.Customization.SystemExtensions = &SchematicSystemExtensions{
			OfficialExtensions: profile.Extensions,
		}
	}

	// Add kernel args if present
	if len(profile.KernelArgs) > 0 {
		schematic.Customization.ExtraKernelArgs = profile.KernelArgs
	}

	// Add overlay if present (SBC boards)
	if profile.Overlay != nil {
		schematic.Overlay = &SchematicOverlay{
			Name:  profile.Overlay.Name,
			Image: profile.Overlay.Image,
		}
	}

	return schematic
}

// GetSchematicID posts a schematic to the factory and returns the ID
func (c *Client) GetSchematicID(schematic *Schematic) (string, error) {
	yamlBytes, err := yaml.Marshal(schematic)
	if err != nil {
		return "", fmt.Errorf("failed to marshal schematic: %w", err)
	}

	resp, err := c.httpClient.Post(
		c.baseURL+"/schematics",
		"application/yaml",
		bytes.NewReader(yamlBytes),
	)
	if err != nil {
		return "", fmt.Errorf("failed to post schematic: %w", err)
	}
	defer resp.Body.Close()

	// Accept both 200 OK and 201 Created
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("factory API returned status %d", resp.StatusCode)
	}

	var result SchematicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if result.ID == "" {
		return "", fmt.Errorf("empty schematic ID returned")
	}

	return result.ID, nil
}

// GetInstallerImage builds the full installer image URL for a profile
func (c *Client) GetInstallerImage(profile config.Profile, version string) (string, error) {
	schematic := BuildSchematic(profile)

	schematicID, err := c.GetSchematicID(schematic)
	if err != nil {
		return "", err
	}

	imageBase := "factory.talos.dev/installer"
	if profile.Secureboot {
		imageBase = "factory.talos.dev/installer-secureboot"
	}

	return fmt.Sprintf("%s/%s:v%s", imageBase, schematicID, version), nil
}

// GenerateFactoryURL builds a browser-friendly URL for the Talos Factory
func GenerateFactoryURL(profile config.Profile, version, baseURL string) string {
	if baseURL == "" {
		baseURL = "https://factory.talos.dev"
	}

	params := url.Values{}
	params.Set("arch", profile.Arch)

	if profile.Overlay != nil {
		// SBC board handling
		params.Set("board", profile.Overlay.Name)
		params.Set("target", "sbc")
	} else {
		params.Set("target", "metal")
		if profile.Secureboot {
			params.Set("secureboot", "true")
		}
	}

	params.Set("platform", profile.Platform)
	params.Set("bootloader", "auto")
	params.Set("cmdline-set", "true")
	params.Set("version", version)

	// Add kernel args
	for _, arg := range profile.KernelArgs {
		params.Add("cmdline", arg)
	}

	// Add extensions (reset first for SBC, then add each)
	if profile.Overlay != nil {
		params.Set("extensions", "-") // Reset default extensions for SBC
	}
	for _, ext := range profile.Extensions {
		params.Add("extensions", ext)
	}

	return fmt.Sprintf("%s/?%s", baseURL, params.Encode())
}

// MockFactoryClient is a mock implementation of FactoryClientInterface for testing
type MockFactoryClient struct {
	GetInstallerImageFunc func(profile config.Profile, version string) (string, error)
	GetSchematicIDFunc    func(schematic *Schematic) (string, error)
}

func (m *MockFactoryClient) GetInstallerImage(profile config.Profile, version string) (string, error) {
	if m.GetInstallerImageFunc != nil {
		return m.GetInstallerImageFunc(profile, version)
	}
	// Default: return a valid-looking image URL
	return fmt.Sprintf("factory.talos.dev/installer/mock-schematic:v%s", version), nil
}

func (m *MockFactoryClient) GetSchematicID(schematic *Schematic) (string, error) {
	if m.GetSchematicIDFunc != nil {
		return m.GetSchematicIDFunc(schematic)
	}
	return "mock-schematic-id", nil
}
