package cmd

import (
	"fmt"
	"sort"

	"github.com/evanjarrett/homelab/internal/talosupgrade/factory"
	"github.com/evanjarrett/homelab/internal/talosupgrade/output"
	"github.com/spf13/cobra"
)

func imagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "images [version]",
		Short: "Generate installer image URLs for each profile",
		Long: `Generate installer image URLs by posting schematics to the Talos Factory API.
These URLs can be used with 'talosctl upgrade --image <URL>'.`,
		Args: cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get version from args or flags
			version := ""
			if len(args) > 0 {
				version = args[0]
			}
			if version == "" {
				var err error
				version, err = getVersion()
				if err != nil {
					return err
				}
			}

			return runImages(version)
		},
	}
}

func runImages(version string) error {
	// Create factory client
	factoryClient := factory.NewClient(cfg.Settings.FactoryBaseURL)
	return runImagesWithClient(factoryClient, version)
}

// runImagesWithClient is the testable core of runImages
func runImagesWithClient(factoryClient factory.FactoryClientInterface, version string) error {
	output.Header("Installer Images for Talos v%s", version)
	fmt.Println()
	fmt.Println("These are the installer image URLs for 'talosctl upgrade --image <URL>'")
	fmt.Println()

	// Sort profile names for consistent output
	var profileNames []string
	for name := range cfg.Profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)

	for _, name := range profileNames {
		profile := cfg.Profiles[name]

		output.SubHeader("Profile: %s", name)

		// Get installer image from factory API
		output.LogInfo("Fetching schematic ID from factory...")
		image, err := factoryClient.GetInstallerImage(profile, version)
		if err != nil {
			output.LogError("Failed to get image for %s: %v", name, err)
			fmt.Println()
			continue
		}

		fmt.Printf("  %s\n", image)
		fmt.Println()

		// Print nodes using this profile
		nodes := cfg.GetNodesByProfile(name)
		if len(nodes) > 0 {
			fmt.Println("  Nodes:")
			for _, node := range nodes {
				fmt.Printf("    - %s (%s)\n", node.IP, node.Role)
			}
			fmt.Println()
		}
	}

	return nil
}
