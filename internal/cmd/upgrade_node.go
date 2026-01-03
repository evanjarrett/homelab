package cmd

import (
	"context"
	"fmt"

	"github.com/evanjarrett/homelab/internal/config"
	"github.com/evanjarrett/homelab/internal/factory"
	"github.com/evanjarrett/homelab/internal/output"
	"github.com/evanjarrett/homelab/internal/talos"
	"github.com/spf13/cobra"
)

func upgradeNodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade-node <ip> [version]",
		Short: "Upgrade a single node",
		Long:  `Upgrade a single node by IP address to the specified version.`,
		Args:  cobra.RangeArgs(1, 2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP := args[0]
			version := ""
			if len(args) > 1 {
				version = args[1]
			}
			return runUpgradeNode(cmd.Context(), nodeIP, version)
		},
	}
}

func runUpgradeNode(ctx context.Context, nodeIP, version string) error {
	// Get version if not specified
	if version == "" {
		var err error
		version, err = getVersion()
		if err != nil {
			return err
		}
	}

	// Create clients
	talosClient, err := talos.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}
	defer talosClient.Close()

	factoryClient := factory.NewClient(cfg.Settings.FactoryBaseURL)

	return runUpgradeNodeWithClients(ctx, talosClient, factoryClient, nodeIP, version)
}

// runUpgradeNodeWithClients is the testable core of runUpgradeNode
func runUpgradeNodeWithClients(ctx context.Context, talosClient talos.TalosClientInterface,
	factoryClient factory.FactoryClientInterface, nodeIP, version string) error {

	// Get or detect the node and profile
	node, profile, profileName, err := getNodeAndProfile(ctx, talosClient, nodeIP)
	if err != nil {
		return err
	}

	output.Header("Upgrading node %s to v%s", nodeIP, version)
	fmt.Println()

	if dryRun {
		output.LogWarn("DRY RUN MODE - No changes will be made")
		fmt.Println()
	}

	// Get installer image
	output.LogInfo("Getting installer image for profile %s...", profileName)
	image, err := factoryClient.GetInstallerImage(*profile, version)
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}
	output.LogSuccess("  %s", image)
	fmt.Println()

	// Run upgrade
	req := UpgradeRequest{
		Node:               *node,
		Image:              image,
		Version:            version,
		ExpectedExtensions: profile.Extensions,
		ExpectedKernelArgs: profile.KernelArgs,
	}
	_, err = upgradeNode(ctx, talosClient, req)
	return err
}

// getNodeAndProfile returns the node and profile for an IP, using detection if available
func getNodeAndProfile(ctx context.Context, client talos.TalosClientInterface, nodeIP string) (*config.Node, *config.Profile, string, error) {
	// Try legacy config first
	if node := cfg.GetNodeByIP(nodeIP); node != nil {
		profile, ok := cfg.Profiles[node.Profile]
		if !ok {
			return nil, nil, "", fmt.Errorf("unknown profile: %s", node.Profile)
		}
		return node, &profile, node.Profile, nil
	}

	// If detection is configured, try to detect
	if cfg.HasDetection() {
		hwInfo, err := client.GetHardwareInfo(ctx, nodeIP)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to get hardware info for %s: %w", nodeIP, err)
		}

		// Convert to config.HardwareInfo for detection
		cfgHwInfo := &config.HardwareInfo{
			SystemManufacturer:    hwInfo.SystemManufacturer,
			SystemProductName:     hwInfo.SystemProductName,
			ProcessorManufacturer: hwInfo.ProcessorManufacturer,
			ProcessorProductName:  hwInfo.ProcessorProductName,
		}

		profileName, profile := cfg.DetectProfile(cfgHwInfo)
		if profile == nil {
			return nil, nil, "", fmt.Errorf("no profile detected for node %s", nodeIP)
		}

		// Get role from cluster members
		members, err := client.GetClusterMembers(ctx)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to get cluster members: %w", err)
		}

		role := config.RoleWorker
		for _, member := range members {
			if member.IP == nodeIP {
				role = member.Role
				break
			}
		}

		node := &config.Node{
			IP:      nodeIP,
			Profile: profileName,
			Role:    role,
		}
		return node, profile, profileName, nil
	}

	return nil, nil, "", fmt.Errorf("unknown node: %s", nodeIP)
}
