package config

import "strings"

// Config represents the complete configuration file
type Config struct {
	Settings  Settings           `yaml:"settings"`
	Profiles  map[string]Profile `yaml:"profiles"`
	Nodes     []Node             `yaml:"nodes,omitempty"`     // Deprecated: use detection rules instead
	Detection *Detection         `yaml:"detection,omitempty"` // Auto-detection configuration
}

// Settings contains global configuration
type Settings struct {
	FactoryBaseURL        string `yaml:"factory_base_url"`
	DefaultTimeoutSeconds int    `yaml:"default_timeout_seconds"`
	DefaultPreserve       bool   `yaml:"default_preserve"`
	GithubReleasesURL     string `yaml:"github_releases_url"`
}

// Profile defines a hardware profile for nodes
type Profile struct {
	Description string   `yaml:"description,omitempty"`
	Arch        string   `yaml:"arch"`
	Platform    string   `yaml:"platform"`
	Secureboot  bool     `yaml:"secureboot"`
	KernelArgs  []string `yaml:"kernel_args,omitempty"`
	Extensions  []string `yaml:"extensions"`
	Overlay     *Overlay `yaml:"overlay,omitempty"`
}

// Overlay defines SBC overlay configuration
type Overlay struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

// Node represents a single cluster node
type Node struct {
	IP      string `yaml:"ip"`
	Profile string `yaml:"profile"`
	Role    string `yaml:"role"` // controlplane, worker
}

// Detection configures automatic profile detection
type Detection struct {
	Rules []DetectionRule `yaml:"rules"`
}

// DetectionRule maps hardware characteristics to a profile
type DetectionRule struct {
	Profile string         `yaml:"profile"`
	Match   DetectionMatch `yaml:"match"`
}

// DetectionMatch defines the hardware characteristics to match
type DetectionMatch struct {
	SystemManufacturer    string `yaml:"system_manufacturer,omitempty"`
	ProcessorManufacturer string `yaml:"processor_manufacturer,omitempty"`
}

// HardwareInfo represents detected hardware information
type HardwareInfo struct {
	SystemManufacturer    string
	SystemProductName     string
	ProcessorManufacturer string
	ProcessorProductName  string
}

// DetectProfile finds the matching profile for given hardware info
func (c *Config) DetectProfile(hw *HardwareInfo) (string, *Profile) {
	if c.Detection == nil || hw == nil {
		return "", nil
	}

	for _, rule := range c.Detection.Rules {
		if matchesRule(hw, &rule.Match) {
			if profile, ok := c.Profiles[rule.Profile]; ok {
				return rule.Profile, &profile
			}
		}
	}
	return "", nil
}

// matchesRule checks if hardware info matches a detection rule
func matchesRule(hw *HardwareInfo, match *DetectionMatch) bool {
	// Check system manufacturer (case-insensitive contains match)
	if match.SystemManufacturer != "" {
		if !strings.Contains(strings.ToLower(hw.SystemManufacturer), strings.ToLower(match.SystemManufacturer)) {
			return false
		}
	}

	// Check processor manufacturer (case-insensitive contains match)
	if match.ProcessorManufacturer != "" {
		if !strings.Contains(strings.ToLower(hw.ProcessorManufacturer), strings.ToLower(match.ProcessorManufacturer)) {
			return false
		}
	}

	// At least one match criterion must be specified
	if match.SystemManufacturer == "" && match.ProcessorManufacturer == "" {
		return false
	}

	return true
}

// NodeRole constants
const (
	RoleControlPlane = "controlplane"
	RoleWorker       = "worker"
)

// GetNodesByRole returns nodes filtered by role
func (c *Config) GetNodesByRole(role string) []Node {
	var nodes []Node
	for _, n := range c.Nodes {
		if n.Role == role {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// GetNodesByProfile returns nodes filtered by profile name
func (c *Config) GetNodesByProfile(profile string) []Node {
	var nodes []Node
	for _, n := range c.Nodes {
		if n.Profile == profile {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// GetNodeByIP returns a node by IP address
func (c *Config) GetNodeByIP(ip string) *Node {
	for _, n := range c.Nodes {
		if n.IP == ip {
			return &n
		}
	}
	return nil
}

// GetProfileForNode returns the profile for a given node IP
func (c *Config) GetProfileForNode(ip string) *Profile {
	node := c.GetNodeByIP(ip)
	if node == nil {
		return nil
	}
	profile, ok := c.Profiles[node.Profile]
	if !ok {
		return nil
	}
	return &profile
}

// GetControlPlaneNodes returns all control plane nodes
func (c *Config) GetControlPlaneNodes() []Node {
	return c.GetNodesByRole(RoleControlPlane)
}

// GetWorkerNodes returns all worker nodes
func (c *Config) GetWorkerNodes() []Node {
	return c.GetNodesByRole(RoleWorker)
}

// GetAllNodesOrdered returns nodes in upgrade order (workers first, then control planes)
func (c *Config) GetAllNodesOrdered() []Node {
	nodes := c.GetWorkerNodes()
	nodes = append(nodes, c.GetControlPlaneNodes()...)
	return nodes
}
