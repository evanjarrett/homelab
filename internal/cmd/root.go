package cmd

import (
	"context"
	"os"

	"github.com/evanjarrett/homelab/internal/config"
	"github.com/evanjarrett/homelab/internal/output"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	cfgFile      string
	dryRun       bool
	talosVersion string
	preserve     bool

	// Loaded config (populated in PreRunE)
	cfg *config.Config

	// Injectable for testing
	configLoader    = config.Load
	versionResolver = config.GetLatestTalosVersion
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "talos-upgrade",
	Short: "Talos cluster upgrade tool",
	Long: `A CLI tool for managing Talos Linux cluster upgrades with profile-based configurations.

This tool supports:
  - Multiple node profiles with different architectures and extensions
  - Generating factory URLs for browser access
  - Generating installer image URLs
  - Upgrading nodes individually or in groups
  - Dry-run mode for testing`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command
func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	// Config file flag
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "",
		"config file (default: configs/talos-profiles.yaml)")

	// Dry run flag
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false,
		"perform a dry run without making changes")

	// Version flag (not the built-in --version, this is the Talos version)
	rootCmd.PersistentFlags().StringVarP(&talosVersion, "talos-version", "V", "",
		"Talos version to upgrade to (default: fetch from GitHub)")

	// Preserve flag
	rootCmd.PersistentFlags().BoolVar(&preserve, "preserve", true,
		"preserve ephemeral data during upgrade")

	// Check environment variables
	if os.Getenv("DRY_RUN") == "true" {
		dryRun = true
	}
	if v := os.Getenv("TALOS_VERSION"); v != "" && talosVersion == "" {
		talosVersion = v
	}
	if os.Getenv("PRESERVE") == "false" {
		preserve = false
	}

	// Add subcommands
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(urlsCmd())
	rootCmd.AddCommand(imagesCmd())
	rootCmd.AddCommand(upgradeCmd())
	rootCmd.AddCommand(upgradeNodeCmd())
}

// loadConfig loads the configuration file
func loadConfig() error {
	return loadConfigWithLoader(configLoader)
}

// loadConfigWithLoader is the testable core of loadConfig
func loadConfigWithLoader(loader func(string) (*config.Config, error)) error {
	var err error
	cfg, err = loader(cfgFile)
	if err != nil {
		return err
	}
	cfg.SetDefaults()
	return nil
}

// getVersion returns the Talos version to use
func getVersion() (string, error) {
	return getVersionWithResolver(versionResolver)
}

// getVersionWithResolver is the testable core of getVersion
func getVersionWithResolver(resolver func(string) (string, error)) (string, error) {
	if talosVersion != "" {
		return talosVersion, nil
	}

	// Fetch from GitHub
	version, err := resolver(cfg.Settings.GithubReleasesURL)
	if err != nil {
		output.LogWarn("Failed to fetch latest version: %v, using fallback", err)
		return "1.9.5", nil // Fallback version
	}
	return version, nil
}
