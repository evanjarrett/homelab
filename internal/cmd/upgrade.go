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

// internalExtensions are system-level extensions that should be ignored during comparison
var internalExtensions = map[string]bool{
	"schematic":   true, // Virtual extension from Image Factory
	"modules.dep": true, // Combined modules.dep for all extensions
}

// UpgradeRequest contains all information needed to upgrade a node
type UpgradeRequest struct {
	Node               config.Node
	Image              string   // Full installer image URL
	Version            string   // Target Talos version
	ExpectedExtensions []string // Extensions from profile
	ExpectedKernelArgs []string // Kernel args from profile
}

// extensionsDiffer compares running extensions with expected extensions from the profile.
// Returns true if there's a difference (upgrade needed), false otherwise.
// Also returns a description of the difference.
func extensionsDiffer(running []talos.ExtensionInfo, expected []string) (bool, string) {
	// Build a set of running extension names (excluding internal extensions)
	runningNames := make(map[string]bool)
	for _, ext := range running {
		if !internalExtensions[ext.Name] {
			runningNames[ext.Name] = true
		}
	}

	// Build a set of expected extension names (strip siderolabs/ prefix)
	expectedNames := make(map[string]bool)
	for _, ext := range expected {
		name := ext
		// Strip vendor prefix (e.g., "siderolabs/gasket-driver" -> "gasket-driver")
		if idx := strings.LastIndex(ext, "/"); idx >= 0 {
			name = ext[idx+1:]
		}
		expectedNames[name] = true
	}

	// Find missing (expected but not running)
	var missing []string
	for name := range expectedNames {
		if !runningNames[name] {
			missing = append(missing, name)
		}
	}

	// Find extra (running but not expected)
	var extra []string
	for name := range runningNames {
		if !expectedNames[name] {
			extra = append(extra, name)
		}
	}

	if len(missing) == 0 && len(extra) == 0 {
		return false, ""
	}

	var parts []string
	if len(missing) > 0 {
		sort.Strings(missing)
		parts = append(parts, fmt.Sprintf("missing: %s", strings.Join(missing, ", ")))
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		parts = append(parts, fmt.Sprintf("extra: %s", strings.Join(extra, ", ")))
	}

	return true, strings.Join(parts, "; ")
}

// kernelArgsDiffer compares running kernel cmdline with expected kernel args from the profile.
// Returns true if there's a difference (upgrade needed), false otherwise.
// Also returns a description of the difference.
func kernelArgsDiffer(cmdline string, expected []string) (bool, string) {
	if len(expected) == 0 {
		return false, ""
	}

	// Split cmdline into individual args
	cmdlineArgs := strings.Fields(cmdline)
	cmdlineSet := make(map[string]bool)
	for _, arg := range cmdlineArgs {
		cmdlineSet[arg] = true
	}

	// Find missing kernel args
	var missing []string
	for _, arg := range expected {
		if !cmdlineSet[arg] {
			missing = append(missing, arg)
		}
	}

	if len(missing) == 0 {
		return false, ""
	}

	sort.Strings(missing)
	return true, fmt.Sprintf("missing kernel args: %s", strings.Join(missing, ", "))
}

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

	// Build list of nodes to upgrade (uses discovery if detection is configured)
	nodes, err := getUpgradeNodes(ctx, talosClient, target)
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
		profile := cfg.Profiles[node.Profile]

		fmt.Println()
		output.Separator()

		req := UpgradeRequest{
			Node:               node,
			Image:              image,
			Version:            version,
			ExpectedExtensions: profile.Extensions,
			ExpectedKernelArgs: profile.KernelArgs,
		}
		skipped, err := upgradeNode(ctx, talosClient, req)
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

// getNodesForTarget returns the list of nodes matching the target (legacy config)
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

// getUpgradeNodes returns nodes for upgrade, using discovery if detection is configured
func getUpgradeNodes(ctx context.Context, client talos.TalosClientInterface, target string) ([]config.Node, error) {
	// If detection is configured, use discovery
	if cfg.HasDetection() {
		return discoverUpgradeNodes(ctx, client, target)
	}

	// Fall back to legacy config
	return getNodesForTarget(target)
}

// discoverUpgradeNodes discovers nodes and builds config.Node structs with detected profiles
func discoverUpgradeNodes(ctx context.Context, client talos.TalosClientInterface, target string) ([]config.Node, error) {
	// Get cluster members
	members, err := client.GetClusterMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover cluster members: %w", err)
	}

	var nodes []config.Node
	for _, member := range members {
		// Get hardware info for profile detection
		hwInfo, err := client.GetHardwareInfo(ctx, member.IP)
		if err != nil {
			return nil, fmt.Errorf("failed to get hardware info for %s: %w", member.IP, err)
		}

		// Convert to config.HardwareInfo for detection
		cfgHwInfo := &config.HardwareInfo{
			SystemManufacturer:    hwInfo.SystemManufacturer,
			SystemProductName:     hwInfo.SystemProductName,
			ProcessorManufacturer: hwInfo.ProcessorManufacturer,
			ProcessorProductName:  hwInfo.ProcessorProductName,
		}

		// Detect profile
		profileName, profile := cfg.DetectProfile(cfgHwInfo)
		if profile == nil {
			return nil, fmt.Errorf("no profile detected for node %s (hw: %+v)", member.IP, cfgHwInfo)
		}

		nodes = append(nodes, config.Node{
			IP:      member.IP,
			Profile: profileName,
			Role:    member.Role,
		})
	}

	// Filter by target
	return filterNodesByTarget(nodes, target)
}

// filterNodesByTarget filters nodes based on target (all, workers, controlplanes, profile, IP)
func filterNodesByTarget(nodes []config.Node, target string) ([]config.Node, error) {
	switch target {
	case "all":
		// Order: workers first, then control planes
		var workers, controlplanes []config.Node
		for _, node := range nodes {
			if node.Role == config.RoleWorker {
				workers = append(workers, node)
			} else {
				controlplanes = append(controlplanes, node)
			}
		}
		return append(workers, controlplanes...), nil

	case "workers":
		var filtered []config.Node
		for _, node := range nodes {
			if node.Role == config.RoleWorker {
				filtered = append(filtered, node)
			}
		}
		return filtered, nil

	case "controlplanes":
		var filtered []config.Node
		for _, node := range nodes {
			if node.Role == config.RoleControlPlane {
				filtered = append(filtered, node)
			}
		}
		return filtered, nil

	default:
		// Check if it's a node IP
		for _, node := range nodes {
			if node.IP == target {
				return []config.Node{node}, nil
			}
		}

		// Check if it's a profile name
		var filtered []config.Node
		for _, node := range nodes {
			if node.Profile == target {
				filtered = append(filtered, node)
			}
		}
		if len(filtered) > 0 {
			return filtered, nil
		}

		return nil, fmt.Errorf("unknown target: %s", target)
	}
}

// upgradeNode upgrades a single node
// Returns (skipped, error) - skipped is true if node was already at target version/extensions/kernel args
func upgradeNode(ctx context.Context, client talos.TalosClientInterface, req UpgradeRequest) (bool, error) {
	// Get current version
	currentVersion, err := client.GetVersion(ctx, req.Node.IP)
	if err != nil {
		currentVersion = "unknown"
	}

	// Get current extensions
	currentExtensions, err := client.GetExtensions(ctx, req.Node.IP)
	if err != nil {
		// Non-fatal: we'll just assume extensions differ
		currentExtensions = nil
	}

	// Get current kernel cmdline
	currentCmdline, err := client.GetKernelCmdline(ctx, req.Node.IP)
	if err != nil {
		// Non-fatal: we'll just assume kernel args differ
		currentCmdline = ""
	}

	// Check if extensions differ
	extDiffer, extDiff := extensionsDiffer(currentExtensions, req.ExpectedExtensions)

	// Check if kernel args differ
	kaDiffer, kaDiff := kernelArgsDiffer(currentCmdline, req.ExpectedKernelArgs)

	// Skip if already at target version AND extensions match AND kernel args match
	if currentVersion == req.Version && !extDiffer && !kaDiffer {
		output.LogSuccess("Node %s already at v%s with matching config, skipping", req.Node.IP, req.Version)
		return true, nil
	}

	output.LogInfo("Upgrading node %s (%s)", req.Node.IP, req.Node.Role)
	output.LogInfo("  Current version: %s", currentVersion)
	if extDiffer {
		output.LogInfo("  Extensions differ: %s", extDiff)
	}
	if kaDiffer {
		output.LogInfo("  Kernel args differ: %s", kaDiff)
	}
	output.LogInfo("  Target image: %s", req.Image)

	if dryRun {
		output.LogWarn("DRY RUN: Would run: talosctl upgrade -n %s --image %s --preserve=%v",
			req.Node.IP, req.Image, preserve)
		return false, nil
	}

	// Run upgrade
	if err := client.Upgrade(ctx, req.Node.IP, req.Image, preserve); err != nil {
		return false, fmt.Errorf("upgrade command failed: %w", err)
	}

	// Watch upgrade progress with streaming events
	output.LogInfo("Watching upgrade progress...")
	timeout := time.Duration(cfg.Settings.DefaultTimeoutSeconds) * time.Second

	var lastPhase, lastTask string
	err = client.WatchUpgrade(ctx, req.Node.IP, timeout, func(p talos.UpgradeProgress) {
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
	if req.Node.Role == config.RoleControlPlane {
		services = talos.GetControlPlaneServices()
	} else {
		services = talos.GetWorkerServices()
	}
	output.LogInfo("Waiting for Talos services: %v", services)
	if err := client.WaitForServices(ctx, req.Node.IP, services, 60*time.Second); err != nil {
		output.LogWarn("Services health check timed out: %v", err)
	} else {
		output.LogSuccess("Talos services healthy")
	}

	// For control plane nodes, also wait for K8s static pods
	if req.Node.Role == config.RoleControlPlane {
		output.LogInfo("Waiting for K8s control plane pods: apiserver, controller-manager, scheduler")
		if err := client.WaitForStaticPods(ctx, req.Node.IP, 90*time.Second); err != nil {
			output.LogWarn("Static pods health check timed out: %v", err)
		} else {
			output.LogSuccess("K8s control plane pods healthy")
		}
	}

	// Verify new version
	newVersion, err := client.GetVersion(ctx, req.Node.IP)
	if err != nil {
		newVersion = "unknown"
	}

	output.LogSuccess("Node %s upgraded: %s -> %s", req.Node.IP, currentVersion, newVersion)
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
