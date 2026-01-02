package cmd

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/evanjarrett/homelab/internal/talosupgrade/output"
	"github.com/evanjarrett/homelab/internal/talosupgrade/talos"
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

	// Collect statuses in parallel
	var (
		statuses []talos.NodeStatus
		mu       sync.Mutex
		wg       sync.WaitGroup
	)

	for _, node := range cfg.Nodes {
		wg.Add(1)
		go func(node struct {
			IP      string
			Profile string
			Role    string
		}) {
			defer wg.Done()

			profile := cfg.Profiles[node.Profile]
			status := client.GetNodeStatus(ctx, node.IP, node.Profile, node.Role, profile.Secureboot)

			mu.Lock()
			statuses = append(statuses, status)
			mu.Unlock()
		}(struct {
			IP      string
			Profile string
			Role    string
		}{node.IP, node.Profile, node.Role})
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
