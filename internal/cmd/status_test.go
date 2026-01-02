package cmd

import (
	"context"
	"sync"
	"testing"

	"github.com/evanjarrett/homelab/internal/config"
	"github.com/evanjarrett/homelab/internal/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// runStatusWithClient() Tests
// ============================================================================

func TestRunStatusWithClient_AllNodesReachable(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 600,
		},
		Profiles: map[string]config.Profile{
			"profile-a": {Arch: "amd64", Platform: "metal", Secureboot: false},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleControlPlane},
			{IP: "192.168.1.2", Profile: "profile-a", Role: config.RoleWorker},
		},
	}

	mock := &talos.MockClient{
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			return talos.NodeStatus{
				IP:         nodeIP,
				Profile:    profile,
				Role:       role,
				Version:    "1.9.0",
				Reachable:  true,
				Secureboot: secureboot,
			}
		},
	}

	err := runStatusWithClient(context.Background(), mock)
	require.NoError(t, err)
}

func TestRunStatusWithClient_SomeNodesUnreachable(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 600,
		},
		Profiles: map[string]config.Profile{
			"profile-a": {Arch: "amd64", Platform: "metal", Secureboot: false},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleControlPlane},
			{IP: "192.168.1.2", Profile: "profile-a", Role: config.RoleWorker},
		},
	}

	mock := &talos.MockClient{
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			reachable := nodeIP == "192.168.1.1" // Only first node is reachable
			version := "1.9.0"
			if !reachable {
				version = "N/A"
			}
			return talos.NodeStatus{
				IP:         nodeIP,
				Profile:    profile,
				Role:       role,
				Version:    version,
				Reachable:  reachable,
				Secureboot: secureboot,
			}
		},
	}

	err := runStatusWithClient(context.Background(), mock)
	require.NoError(t, err)
}

func TestRunStatusWithClient_MixedRoles(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 600,
		},
		Profiles: map[string]config.Profile{
			"profile-a": {Arch: "amd64", Platform: "metal", Secureboot: false},
			"profile-b": {Arch: "arm64", Platform: "metal", Secureboot: true},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "profile-a", Role: config.RoleControlPlane},
			{IP: "192.168.1.2", Profile: "profile-a", Role: config.RoleWorker},
			{IP: "192.168.1.3", Profile: "profile-b", Role: config.RoleControlPlane},
			{IP: "192.168.1.4", Profile: "profile-b", Role: config.RoleWorker},
		},
	}

	var mu sync.Mutex
	calledWith := make(map[string]bool)
	mock := &talos.MockClient{
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			mu.Lock()
			calledWith[nodeIP] = true
			mu.Unlock()
			return talos.NodeStatus{
				IP:         nodeIP,
				Profile:    profile,
				Role:       role,
				Version:    "1.9.0",
				Reachable:  true,
				Secureboot: secureboot,
			}
		},
	}

	err := runStatusWithClient(context.Background(), mock)
	require.NoError(t, err)

	// All nodes should have been queried
	assert.True(t, calledWith["192.168.1.1"])
	assert.True(t, calledWith["192.168.1.2"])
	assert.True(t, calledWith["192.168.1.3"])
	assert.True(t, calledWith["192.168.1.4"])
}

func TestRunStatusWithClient_SecurebootIndicators(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 600,
		},
		Profiles: map[string]config.Profile{
			"secureboot":    {Arch: "amd64", Platform: "metal", Secureboot: true},
			"no-secureboot": {Arch: "amd64", Platform: "metal", Secureboot: false},
		},
		Nodes: []config.Node{
			{IP: "192.168.1.1", Profile: "secureboot", Role: config.RoleControlPlane},
			{IP: "192.168.1.2", Profile: "no-secureboot", Role: config.RoleWorker},
		},
	}

	var mu sync.Mutex
	securebootNodes := make(map[string]bool)
	mock := &talos.MockClient{
		GetNodeStatusFunc: func(ctx context.Context, nodeIP, profile, role string, secureboot bool) talos.NodeStatus {
			mu.Lock()
			securebootNodes[nodeIP] = secureboot
			mu.Unlock()
			return talos.NodeStatus{
				IP:         nodeIP,
				Profile:    profile,
				Role:       role,
				Version:    "1.9.0",
				Reachable:  true,
				Secureboot: secureboot,
			}
		},
	}

	err := runStatusWithClient(context.Background(), mock)
	require.NoError(t, err)

	// Verify correct secureboot values were passed
	assert.True(t, securebootNodes["192.168.1.1"], "secureboot profile should have secureboot=true")
	assert.False(t, securebootNodes["192.168.1.2"], "no-secureboot profile should have secureboot=false")
}

func TestRunStatusWithClient_EmptyConfig(t *testing.T) {
	cfg = &config.Config{
		Settings: config.Settings{
			FactoryBaseURL:        "https://factory.talos.dev",
			DefaultTimeoutSeconds: 600,
		},
		Profiles: map[string]config.Profile{},
		Nodes:    []config.Node{},
	}

	mock := &talos.MockClient{}

	err := runStatusWithClient(context.Background(), mock)
	require.NoError(t, err)
}
