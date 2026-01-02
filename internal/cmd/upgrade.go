package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/evanjarrett/homelab/internal/config"
	"github.com/evanjarrett/homelab/internal/factory"
	"github.com/evanjarrett/homelab/internal/output"
	"github.com/evanjarrett/homelab/internal/talos"
	"github.com/spf13/cobra"
)

func upgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade [target] [version]",
		Short: "Upgrade nodes to specified version",
		Long: `Upgrade nodes to specified version.

Target can be:
  all          - All nodes (workers first, then control planes)
  workers      - Worker nodes only
  controlplanes - Control plane nodes only
  <profile>    - Nodes matching a specific profile
  <ip>         - A single node by IP address

Arguments can be in either order:
  upgrade 1.9.5 workers
  upgrade workers 1.9.5`,
		Args: cobra.MaximumNArgs(2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target, version := parseUpgradeArgs(args)
			return runUpgrade(cmd.Context(), target, version)
		},
	}
}

// parseUpgradeArgs handles flexible argument ordering
func parseUpgradeArgs(args []string) (target, version string) {
	target = "all"
	version = ""

	if len(args) == 0 {
		return
	}

	// Check if first arg looks like a version (starts with digit)
	if len(args) >= 1 {
		if isVersion(args[0]) {
			version = args[0]
			if len(args) >= 2 {
				target = args[1]
			}
		} else {
			target = args[0]
			if len(args) >= 2 {
				version = args[1]
			}
		}
	}

	return
}

// isVersion checks if a string looks like a version number (e.g., 1.12.0)
// rather than an IP address (e.g., 192.168.1.161)
func isVersion(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Must start with a digit
	if s[0] < '0' || s[0] > '9' {
		return false
	}
	// Count dots - versions have 2 dots (X.Y.Z), IPs have 3 dots (A.B.C.D)
	dots := 0
	for _, c := range s {
		if c == '.' {
			dots++
		}
	}
	// Version: 1.12.0 (2 dots), IP: 192.168.1.161 (3 dots)
	return dots <= 2
}

func runUpgrade(ctx context.Context, target, version string) error {
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

	return runUpgradeWithClients(ctx, talosClient, factoryClient, target, version)
}

// runUpgradeWithClients is the testable core of runUpgrade
func runUpgradeWithClients(ctx context.Context, talosClient talos.TalosClientInterface,
	factoryClient factory.FactoryClientInterface, target, version string) error {

	output.Header("Talos Cluster Upgrade to v%s", version)
	fmt.Println()

	if dryRun {
		output.LogWarn("DRY RUN MODE - No changes will be made")
		fmt.Println()
	}

	// Build list of nodes to upgrade
	nodes, err := getNodesForTarget(target)
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		output.LogError("No nodes found to upgrade for target: %s", target)
		return fmt.Errorf("no nodes found")
	}

	// Sort nodes by IP
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].IP < nodes[j].IP
	})

	// Print nodes to upgrade
	fmt.Println("Nodes to upgrade (in order):")
	for _, node := range nodes {
		fmt.Printf("  - %s (%s, profile: %s)\n", node.IP, node.Role, node.Profile)
	}
	fmt.Println()

	// Confirm unless dry run
	if !dryRun {
		if !confirm("Proceed with upgrade?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Get installer images for each profile (cache them)
	profileImages := make(map[string]string)
	profilesNeeded := make(map[string]bool)
	for _, node := range nodes {
		profilesNeeded[node.Profile] = true
	}

	for profileName := range profilesNeeded {
		profile := cfg.Profiles[profileName]
		output.LogInfo("Getting installer image for profile %s...", profileName)

		image, err := factoryClient.GetInstallerImage(profile, version)
		if err != nil {
			return fmt.Errorf("failed to get image for profile %s: %w", profileName, err)
		}

		profileImages[profileName] = image
		output.LogSuccess("  %s", image)
	}
	fmt.Println()

	// Upgrade nodes one by one
	var failedNodes []string
	var skippedNodes []string
	for _, node := range nodes {
		image := profileImages[node.Profile]

		fmt.Println()
		output.Separator()

		skipped, err := upgradeNode(ctx, talosClient, node, image, version)
		if err != nil {
			failedNodes = append(failedNodes, node.IP)
			output.LogError("Failed to upgrade %s: %v", node.IP, err)

			// For control plane failures, ask before continuing
			if node.Role == config.RoleControlPlane && !dryRun {
				if !confirm("Control plane upgrade failed. Continue?") {
					break
				}
			}
		} else if skipped {
			skippedNodes = append(skippedNodes, node.IP)
		}
	}

	// Summary
	fmt.Println()
	output.Separator()
	output.Header("Upgrade Summary")
	fmt.Println()

	if len(skippedNodes) > 0 {
		output.LogInfo("Skipped (already at target): %s", strings.Join(skippedNodes, ", "))
	}
	if len(failedNodes) > 0 {
		output.LogError("Failed nodes: %s", strings.Join(failedNodes, ", "))
	}
	if len(failedNodes) == 0 {
		output.LogSuccess("All nodes upgraded successfully!")
	}

	// Show final status
	fmt.Println()
	return runStatusWithClient(ctx, talosClient)
}

// getNodesForTarget returns the list of nodes matching the target
func getNodesForTarget(target string) ([]config.Node, error) {
	switch target {
	case "all":
		return cfg.GetAllNodesOrdered(), nil
	case "workers":
		return cfg.GetWorkerNodes(), nil
	case "controlplanes":
		return cfg.GetControlPlaneNodes(), nil
	default:
		// Check if it's a node IP
		if node := cfg.GetNodeByIP(target); node != nil {
			return []config.Node{*node}, nil
		}

		// Check if it's a profile name
		nodes := cfg.GetNodesByProfile(target)
		if len(nodes) > 0 {
			return nodes, nil
		}

		return nil, fmt.Errorf("unknown target: %s", target)
	}
}

// upgradeNode upgrades a single node
// Returns (skipped, error) - skipped is true if node was already at target version
func upgradeNode(ctx context.Context, client talos.TalosClientInterface, node config.Node, image, version string) (bool, error) {
	// Get current version
	currentVersion, err := client.GetVersion(ctx, node.IP)
	if err != nil {
		currentVersion = "unknown"
	}

	// Skip if already at target version
	if currentVersion == version {
		output.LogSuccess("Node %s already at v%s, skipping", node.IP, version)
		return true, nil
	}

	output.LogInfo("Upgrading node %s (%s)", node.IP, node.Role)
	output.LogInfo("  Current version: %s", currentVersion)
	output.LogInfo("  Target image: %s", image)

	if dryRun {
		output.LogWarn("DRY RUN: Would run: talosctl upgrade -n %s --image %s --preserve=%v",
			node.IP, image, preserve)
		return false, nil
	}

	// Run upgrade
	if err := client.Upgrade(ctx, node.IP, image, preserve); err != nil {
		return false, fmt.Errorf("upgrade command failed: %w", err)
	}

	// Watch upgrade progress with streaming events
	output.LogInfo("Watching upgrade progress...")
	timeout := time.Duration(cfg.Settings.DefaultTimeoutSeconds) * time.Second

	var lastPhase, lastTask string
	err = client.WatchUpgrade(ctx, node.IP, timeout, func(p talos.UpgradeProgress) {
		// Show stage changes
		if p.Stage != "" {
			output.LogInfo("  [%s]", p.Stage)
		}
		// Show phase changes (avoid duplicates)
		if p.Phase != "" && p.Phase != lastPhase {
			lastPhase = p.Phase
			output.LogInfo("    phase: %s (%s)", p.Phase, p.Action)
		}
		// Show task changes (avoid duplicates)
		if p.Task != "" && p.Task != lastTask {
			lastTask = p.Task
			output.LogInfo("      task: %s (%s)", p.Task, p.Action)
		}
	})
	if err != nil {
		return false, fmt.Errorf("upgrade failed: %w", err)
	}

	// Wait for critical services to be healthy
	var services []string
	if node.Role == config.RoleControlPlane {
		services = talos.GetControlPlaneServices()
	} else {
		services = talos.GetWorkerServices()
	}
	output.LogInfo("Waiting for Talos services: %v", services)
	if err := client.WaitForServices(ctx, node.IP, services, 60*time.Second); err != nil {
		output.LogWarn("Services health check timed out: %v", err)
	} else {
		output.LogSuccess("Talos services healthy")
	}

	// For control plane nodes, also wait for K8s static pods
	if node.Role == config.RoleControlPlane {
		output.LogInfo("Waiting for K8s control plane pods: apiserver, controller-manager, scheduler")
		if err := client.WaitForStaticPods(ctx, node.IP, 90*time.Second); err != nil {
			output.LogWarn("Static pods health check timed out: %v", err)
		} else {
			output.LogSuccess("K8s control plane pods healthy")
		}
	}

	// Verify new version
	newVersion, err := client.GetVersion(ctx, node.IP)
	if err != nil {
		newVersion = "unknown"
	}

	output.LogSuccess("Node %s upgraded: %s -> %s", node.IP, currentVersion, newVersion)
	return false, nil
}

// confirmReader is the reader used for confirmation prompts (can be replaced in tests)
var confirmReader io.Reader = os.Stdin

// confirm asks the user for confirmation
func confirm(prompt string) bool {
	reader := bufio.NewReader(confirmReader)
	fmt.Printf("%s [y/N] ", prompt)

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
