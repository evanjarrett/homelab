package cmd

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/evanjarrett/homelab/internal/config"
	"github.com/evanjarrett/homelab/internal/output"
	"github.com/evanjarrett/homelab/internal/talos"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current cluster status and node versions",
		Long:  `Display the current status of all nodes in the cluster, including versions and reachability.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context())
		},
	}
}

func runStatus(ctx context.Context) error {
	// Create Talos client
	talosClient, err := talos.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}
	defer talosClient.Close()

	return runStatusWithClient(ctx, talosClient)
}

// runStatusWithClient is the testable core of runStatus
func runStatusWithClient(ctx context.Context, client talos.TalosClientInterface) error {
	output.Header("Talos Cluster Status")
	fmt.Println()

	// Get nodes - either from discovery or legacy config
	nodes, err := getStatusNodes(ctx, client)
	if err != nil {
		return err
	}

	// Collect statuses in parallel
	var (
		statuses []talos.NodeStatus
		mu       sync.Mutex
		wg       sync.WaitGroup
	)

	for _, node := range nodes {
		wg.Add(1)
		go func(node statusNode) {
			defer wg.Done()

			status := client.GetNodeStatus(ctx, node.IP, node.Profile, node.Role, node.Secureboot)

			mu.Lock()
			statuses = append(statuses, status)
			mu.Unlock()
		}(node)
	}

	wg.Wait()

	// Sort by IP (last octet)
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].IP < statuses[j].IP
	})

	// Print table
	tw := output.NewTabWriter()
	fmt.Fprintf(tw, "NODE\tTYPE\tPROFILE\tVERSION\tSECBOOT\tSTATUS\n")
	fmt.Fprintf(tw, "----\t----\t-------\t-------\t-------\t------\n")

	for _, s := range statuses {
		statusStr := "OK"
		if !s.Reachable {
			statusStr = "UNREACHABLE"
		}

		secbootStr := "no"
		if s.Secureboot {
			secbootStr = "yes"
		}

		statusColor := output.StatusColor(statusStr)
		roleColor := output.RoleColor(s.Role)

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			s.IP,
			roleColor(s.Role),
			s.Profile,
			s.Version,
			secbootStr,
			statusColor(statusStr),
		)
	}

	tw.Flush()
	return nil
}

// statusNode represents a node for status display
type statusNode struct {
	IP         string
	Profile    string
	Role       string
	Secureboot bool
}

// getStatusNodes returns nodes to check, using discovery if detection is configured
func getStatusNodes(ctx context.Context, client talos.TalosClientInterface) ([]statusNode, error) {
	// If detection is configured, use discovery
	if cfg.HasDetection() {
		return discoverNodes(ctx, client)
	}

	// Fall back to legacy config nodes
	var nodes []statusNode
	for _, node := range cfg.Nodes {
		profile := cfg.Profiles[node.Profile]
		nodes = append(nodes, statusNode{
			IP:         node.IP,
			Profile:    node.Profile,
			Role:       node.Role,
			Secureboot: profile.Secureboot,
		})
	}
	return nodes, nil
}

// discoverNodes uses Talos API to discover nodes and detect their profiles
func discoverNodes(ctx context.Context, client talos.TalosClientInterface) ([]statusNode, error) {
	// Get cluster members
	members, err := client.GetClusterMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover cluster members: %w", err)
	}

	var nodes []statusNode
	for _, member := range members {
		// Get hardware info for profile detection
		hwInfo, err := client.GetHardwareInfo(ctx, member.IP)
		if err != nil {
			// If we can't get hardware info, use unknown profile
			nodes = append(nodes, statusNode{
				IP:         member.IP,
				Profile:    "unknown",
				Role:       member.Role,
				Secureboot: false,
			})
			continue
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
			profileName = "unknown"
		}

		secureboot := false
		if profile != nil {
			secureboot = profile.Secureboot
		}

		nodes = append(nodes, statusNode{
			IP:         member.IP,
			Profile:    profileName,
			Role:       member.Role,
			Secureboot: secureboot,
		})
	}

	return nodes, nil
}
