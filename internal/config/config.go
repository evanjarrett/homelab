package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPaths defines where to search for config files
var DefaultConfigPaths = []string{
	"configs/talos-profiles.yaml",
	"talos-profiles.yaml",
}

// Load reads and parses the configuration file
func Load(path string) (*Config, error) {
	if path == "" {
		path = findDefaultConfig()
		if path == "" {
			return nil, fmt.Errorf("no config file found, tried: %v", DefaultConfigPaths)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// findDefaultConfig searches for config in default locations
func findDefaultConfig() string {
	// First check relative to current directory
	for _, p := range DefaultConfigPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Check relative to executable location
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		for _, p := range DefaultConfigPaths {
			fullPath := filepath.Join(exeDir, p)
			if _, err := os.Stat(fullPath); err == nil {
				return fullPath
			}
		}
	}

	return ""
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	if len(c.Profiles) == 0 {
		return fmt.Errorf("no profiles defined")
	}

	if len(c.Nodes) == 0 {
		return fmt.Errorf("no nodes defined")
	}

	// Validate each profile
	for name, profile := range c.Profiles {
		if profile.Arch == "" {
			return fmt.Errorf("profile %s: arch is required", name)
		}
		if profile.Platform == "" {
			return fmt.Errorf("profile %s: platform is required", name)
		}
	}

	// Validate each node references a valid profile
	for _, node := range c.Nodes {
		if node.IP == "" {
			return fmt.Errorf("node with empty IP found")
		}
		if node.Profile == "" {
			return fmt.Errorf("node %s: profile is required", node.IP)
		}
		if _, ok := c.Profiles[node.Profile]; !ok {
			return fmt.Errorf("node %s: references unknown profile %s", node.IP, node.Profile)
		}
		if node.Role != RoleControlPlane && node.Role != RoleWorker {
			return fmt.Errorf("node %s: role must be 'controlplane' or 'worker', got %s", node.IP, node.Role)
		}
	}

	return nil
}

// SetDefaults fills in default values for settings
func (c *Config) SetDefaults() {
	if c.Settings.FactoryBaseURL == "" {
		c.Settings.FactoryBaseURL = "https://factory.talos.dev"
	}
	if c.Settings.DefaultTimeoutSeconds == 0 {
		c.Settings.DefaultTimeoutSeconds = 600
	}
	if c.Settings.GithubReleasesURL == "" {
		c.Settings.GithubReleasesURL = "https://api.github.com/repos/siderolabs/talos/releases/latest"
	}
}

// githubRelease represents the GitHub API response for releases
type githubRelease struct {
	TagName string `json:"tag_name"`
}

// GetLatestTalosVersion fetches the latest Talos version from GitHub
func GetLatestTalosVersion(githubURL string) (string, error) {
	if githubURL == "" {
		githubURL = "https://api.github.com/repos/siderolabs/talos/releases/latest"
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(githubURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	// Strip leading 'v' if present
	version := release.TagName
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}

	if version == "" {
		return "", fmt.Errorf("empty version returned from GitHub")
	}

	return version, nil
}
