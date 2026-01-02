package cmd

import (
	"context"
	"fmt"

	"github.com/evanjarrett/homelab/internal/talosupgrade/factory"
	"github.com/evanjarrett/homelab/internal/talosupgrade/output"
	"github.com/evanjarrett/homelab/internal/talosupgrade/talos"
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

	// Find the node
	node := cfg.GetNodeByIP(nodeIP)
	if node == nil {
		output.LogError("Unknown node: %s", nodeIP)
		return fmt.Errorf("unknown node: %s", nodeIP)
	}

	// Get the profile
	profile, ok := cfg.Profiles[node.Profile]
	if !ok {
		return fmt.Errorf("unknown profile: %s", node.Profile)
	}

	output.Header("Upgrading node %s to v%s", nodeIP, version)
	fmt.Println()

	if dryRun {
		output.LogWarn("DRY RUN MODE - No changes will be made")
		fmt.Println()
	}

	// Get installer image
	output.LogInfo("Getting installer image for profile %s...", node.Profile)
	image, err := factoryClient.GetInstallerImage(profile, version)
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}
	output.LogSuccess("  %s", image)
	fmt.Println()

	// Run upgrade
	_, err = upgradeNode(ctx, talosClient, *node, image, version)
	return err
}
