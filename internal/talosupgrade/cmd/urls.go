package cmd

import (
	"fmt"
	"sort"

	"github.com/evanjarrett/homelab/internal/talosupgrade/factory"
	"github.com/evanjarrett/homelab/internal/talosupgrade/output"
	"github.com/spf13/cobra"
)

func urlsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "urls [version]",
		Short: "Generate factory URLs for each profile (for browser)",
		Long: `Generate factory.talos.dev URLs that can be opened in a browser
to download images or get installer commands for each profile.`,
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

			return runURLs(version)
		},
	}
}

func runURLs(version string) error {
	output.Header("Factory URLs for Talos v%s", version)
	fmt.Println()
	fmt.Println("Open these URLs in a browser to download images or get installer commands.")
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

		// Print profile info
		fmt.Printf("  Arch: %s, Secureboot: %v\n", profile.Arch, profile.Secureboot)
		if profile.Overlay != nil {
			fmt.Printf("  Overlay: %s\n", profile.Overlay.Name)
		}
		if len(profile.KernelArgs) > 0 {
			fmt.Printf("  Kernel Args: %v\n", profile.KernelArgs)
		}
		fmt.Printf("  Extensions: %v\n", profile.Extensions)
		fmt.Println()

		// Generate and print URL
		url := factory.GenerateFactoryURL(profile, version, cfg.Settings.FactoryBaseURL)
		fmt.Printf("  URL:\n%s\n", url)
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
