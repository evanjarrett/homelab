package talos

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// NodeStatus represents the status of a single node
type NodeStatus struct {
	IP         string
	Profile    string
	Role       string
	Version    string
	MachineType string
	Secureboot bool
	Reachable  bool
}

// Client wraps the Talos SDK client
type Client struct {
	talos TalosMachineClient
	k8s   kubernetes.Interface
	clock Clock
}

// NewClient creates a new Talos client using default config
func NewClient(ctx context.Context) (*Client, error) {
	talosClient, err := talosclient.New(ctx, talosclient.WithDefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos client: %w", err)
	}

	// Wrap the SDK client
	wrapper := newTalosClientWrapper(talosClient)

	// Load kubeconfig for node ready checks
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		// K8s client is optional - we can still work without it
		return &Client{talos: wrapper, k8s: nil, clock: newRealClock()}, nil
	}

	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		// K8s client is optional
		return &Client{talos: wrapper, k8s: nil, clock: newRealClock()}, nil
	}

	return &Client{talos: wrapper, k8s: k8sClient, clock: newRealClock()}, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	if c.talos != nil {
		return c.talos.Close()
	}
	return nil
}

// GetVersion retrieves the Talos version for a node
func (c *Client) GetVersion(ctx context.Context, nodeIP string) (string, error) {
	nodeCtx := talosclient.WithNode(ctx, nodeIP)
	resp, err := c.talos.Version(nodeCtx)
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}

	// Extract version from response
	for _, msg := range resp.Messages {
		if msg.Version != nil {
			tag := msg.Version.Tag
			// Strip leading 'v' if present
			if len(tag) > 0 && tag[0] == 'v' {
				tag = tag[1:]
			}
			return tag, nil
		}
	}
	return "", fmt.Errorf("no version in response")
}

// GetMachineType retrieves the machine type (controlplane/worker) for a node
// This is inferred from the Version response metadata since the COSI API
// requires specific resource definitions that are internal to Talos
func (c *Client) GetMachineType(ctx context.Context, nodeIP string) (string, error) {
	nodeCtx := talosclient.WithNode(ctx, nodeIP)

	// The Version response includes platform info that can help identify node type
	// For now, we rely on the config to provide the role
	resp, err := c.talos.Version(nodeCtx)
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}

	// Check if any message indicates this is a control plane
	for _, msg := range resp.Messages {
		if msg.Platform != nil {
			// Platform info is available but doesn't directly indicate role
			// The role is best obtained from config
			break
		}
	}

	// Return unknown - the caller should use the role from config
	return "unknown", nil
}

// IsReachable checks if a node is reachable via the Talos API
func (c *Client) IsReachable(ctx context.Context, nodeIP string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := c.GetVersion(ctx, nodeIP)
	return err == nil
}

// Upgrade performs an upgrade on a node
func (c *Client) Upgrade(ctx context.Context, nodeIP, image string, preserve bool) error {
	nodeCtx := talosclient.WithNode(ctx, nodeIP)

	_, err := c.talos.UpgradeWithOptions(
		nodeCtx,
		talosclient.WithUpgradeImage(image),
		talosclient.WithUpgradePreserve(preserve),
	)
	if err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}

	return nil
}

// WaitForNode waits for a node to be ready after upgrade
func (c *Client) WaitForNode(ctx context.Context, nodeIP string, timeout time.Duration) error {
	clock := c.clock
	if clock == nil {
		clock = newRealClock()
	}
	deadline := clock.Now().Add(timeout)

	for clock.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check Talos API reachability
		if c.IsReachable(ctx, nodeIP) {
			// Check Kubernetes node ready status if k8s client is available
			if c.k8s == nil || c.isK8sNodeReady(ctx, nodeIP) {
				return nil
			}
		}

		clock.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for node %s", nodeIP)
}

// isK8sNodeReady checks if the Kubernetes node is in Ready state
func (c *Client) isK8sNodeReady(ctx context.Context, nodeIP string) bool {
	if c.k8s == nil {
		return true // Skip check if no k8s client
	}

	nodes, err := c.k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false
	}

	for _, node := range nodes.Items {
		// Check if this node matches the IP
		for _, addr := range node.Status.Addresses {
			if addr.Address == nodeIP {
				// Check Ready condition
				for _, cond := range node.Status.Conditions {
					if cond.Type == "Ready" && cond.Status == "True" {
						return true
					}
				}
			}
		}
	}

	return false
}

// GetK8sNodeName returns the Kubernetes node name for an IP
func (c *Client) GetK8sNodeName(ctx context.Context, nodeIP string) string {
	if c.k8s == nil {
		return ""
	}

	nodes, err := c.k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return ""
	}

	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Address == nodeIP {
				return node.Name
			}
		}
	}

	return ""
}

// GetNodeStatus retrieves comprehensive status for a node
func (c *Client) GetNodeStatus(ctx context.Context, nodeIP, profile, role string, secureboot bool) NodeStatus {
	status := NodeStatus{
		IP:         nodeIP,
		Profile:    profile,
		Role:       role,
		Secureboot: secureboot,
		Reachable:  false,
		Version:    "N/A",
		MachineType: "unknown",
	}

	// Check reachability first
	if !c.IsReachable(ctx, nodeIP) {
		return status
	}
	status.Reachable = true

	// Get version
	if version, err := c.GetVersion(ctx, nodeIP); err == nil {
		status.Version = version
	}

	// Get machine type
	if machineType, err := c.GetMachineType(ctx, nodeIP); err == nil {
		status.MachineType = strings.ToLower(machineType)
	}

	return status
}

// UpgradeProgress represents the current state of an upgrade
type UpgradeProgress struct {
	Stage   string // Machine stage: upgrading, rebooting, booting, running
	Phase   string // Current phase name
	Task    string // Current task name
	Action  string // START or STOP
	Error   string // Error message if any
	Done    bool   // True when upgrade is complete (node is running)
}

// ProgressCallback is called for each upgrade progress event
type ProgressCallback func(UpgradeProgress)

// WatchUpgrade streams upgrade events and calls the callback for each event.
// It handles the node reboot by reconnecting and waiting for the node to reach RUNNING state.
// Since the upgrade has already been initiated before this is called, any disconnect
// means the node is rebooting and we should wait for it to come back.
func (c *Client) WatchUpgrade(ctx context.Context, nodeIP string, timeout time.Duration, onProgress ProgressCallback) error {
	clock := c.clock
	if clock == nil {
		clock = newRealClock()
	}
	deadline := clock.Now().Add(timeout)
	nodeCtx := talosclient.WithNode(ctx, nodeIP)

	// Start watching events
	eventCh := make(chan talosclient.EventResult, 100)

	// Start event watcher in goroutine
	watchCtx, watchCancel := context.WithCancel(nodeCtx)
	defer watchCancel()

	go func() {
		// Watch from now (tail -1 means all new events)
		// Note: Do NOT close eventCh - the SDK's internal goroutine may still send to it
		// after this function returns. Let it be garbage collected.
		c.talos.EventsWatchV2(watchCtx, eventCh, talosclient.WithTailEvents(-1))
	}()

	// Process events until done or timeout
	// Since upgrade was already initiated, any disconnect means we wait for reboot
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-clock.After(deadline.Sub(clock.Now())):
			return fmt.Errorf("timeout waiting for upgrade to complete on %s", nodeIP)

		case result := <-eventCh:
			if result.Error != nil {
				// Stream error - node is likely rebooting
				if onProgress != nil {
					onProgress(UpgradeProgress{Stage: "rebooting", Action: "connection lost"})
				}
				return c.waitForRunning(ctx, nodeIP, deadline.Sub(clock.Now()), onProgress)
			}

			// Process the event
			progress := c.parseEvent(result.Event)
			if progress != nil {
				// Call the callback
				if onProgress != nil {
					onProgress(*progress)
				}

				// Check if done
				if progress.Done {
					return nil
				}

				// Check for error in the event
				if progress.Error != "" {
					return fmt.Errorf("upgrade failed: %s", progress.Error)
				}
			}
		}
	}
}

// parseEvent converts a Talos event to UpgradeProgress
func (c *Client) parseEvent(event talosclient.Event) *UpgradeProgress {
	progress := &UpgradeProgress{}

	switch e := event.Payload.(type) {
	case *machine.SequenceEvent:
		progress.Phase = e.GetSequence()
		progress.Action = e.GetAction().String()
		if e.GetError() != nil {
			progress.Error = e.GetError().GetMessage()
		}
		return progress

	case *machine.PhaseEvent:
		progress.Phase = e.GetPhase()
		progress.Action = e.GetAction().String()
		return progress

	case *machine.TaskEvent:
		progress.Task = e.GetTask()
		progress.Action = e.GetAction().String()
		return progress

	case *machine.MachineStatusEvent:
		stage := e.GetStage()
		progress.Stage = strings.ToLower(stage.String())

		// Check if we're done (node is running)
		if stage == machine.MachineStatusEvent_RUNNING {
			progress.Done = true
		}
		return progress

	default:
		// Unknown event type, ignore
		return nil
	}
}

// WaitForServices waits for critical Talos services to be healthy
func (c *Client) WaitForServices(ctx context.Context, nodeIP string, services []string, timeout time.Duration) error {
	clock := c.clock
	if clock == nil {
		clock = newRealClock()
	}
	deadline := clock.Now().Add(timeout)
	nodeCtx := talosclient.WithNode(ctx, nodeIP)

	for clock.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		allHealthy := true
		for _, svc := range services {
			resp, err := c.talos.ServiceInfo(nodeCtx, svc)
			if err != nil {
				allHealthy = false
				break
			}

			// Check if service is running and healthy
			// resp is []ServiceInfo with Service field containing the actual info
			for _, svcInfo := range resp {
				if svcInfo.Service != nil && svcInfo.Service.Id == svc {
					if svcInfo.Service.State != "Running" || (svcInfo.Service.Health != nil && !svcInfo.Service.Health.Healthy) {
						allHealthy = false
					}
				}
			}
		}

		if allHealthy {
			return nil
		}

		clock.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for services to be healthy")
}

// GetControlPlaneServices returns the list of services to wait for on control plane nodes
func GetControlPlaneServices() []string {
	return []string{"etcd", "kubelet", "apid", "trustd"}
}

// GetWorkerServices returns the list of services to wait for on worker nodes
func GetWorkerServices() []string {
	return []string{"kubelet", "apid", "trustd"}
}

// WaitForStaticPods waits for K8s control plane static pods to be healthy
func (c *Client) WaitForStaticPods(ctx context.Context, nodeIP string, timeout time.Duration) error {
	clock := c.clock
	if clock == nil {
		clock = newRealClock()
	}
	deadline := clock.Now().Add(timeout)
	nodeCtx := talosclient.WithNode(ctx, nodeIP)

	requiredPods := []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler"}

	for clock.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// List static pod statuses via COSI
		list, err := c.talos.COSIList(nodeCtx, resource.NewMetadata(k8s.NamespaceName, k8s.StaticPodStatusType, "", resource.VersionUndefined))
		if err != nil {
			clock.Sleep(2 * time.Second)
			continue
		}

		// Check if all required pods are ready
		readyPods := make(map[string]bool)
		for _, res := range list.Items {
			status, ok := res.(*k8s.StaticPodStatus)
			if !ok {
				continue
			}

			// Extract pod name from ID (e.g., "kube-system/kube-apiserver-node")
			id := res.Metadata().ID()
			for _, required := range requiredPods {
				if strings.Contains(id, required) {
					// Check if pod is ready
					podStatus := status.TypedSpec().PodStatus
					if phase, ok := podStatus["phase"].(string); ok && phase == "Running" {
						// Check conditions for Ready
						if conditions, ok := podStatus["conditions"].([]any); ok {
							for _, cond := range conditions {
								if condMap, ok := cond.(map[string]any); ok {
									if condMap["type"] == "Ready" && condMap["status"] == "True" {
										readyPods[required] = true
									}
								}
							}
						}
					}
				}
			}
		}

		// Check if all required pods are ready
		allReady := true
		for _, pod := range requiredPods {
			if !readyPods[pod] {
				allReady = false
				break
			}
		}

		if allReady {
			return nil
		}

		clock.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for static pods to be healthy")
}

// waitForRunning polls for the node to come back up after reboot
func (c *Client) waitForRunning(ctx context.Context, nodeIP string, remaining time.Duration, onProgress ProgressCallback) error {
	clock := c.clock
	if clock == nil {
		clock = newRealClock()
	}
	if onProgress != nil {
		onProgress(UpgradeProgress{Stage: "rebooting", Action: "waiting for node to come back"})
	}

	deadline := clock.Now().Add(remaining)
	pollInterval := 2 * time.Second

	for clock.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-clock.After(pollInterval):
		}

		// Try to connect and check status
		if c.IsReachable(ctx, nodeIP) {
			// Node is back - try to watch for RUNNING state
			nodeCtx := talosclient.WithNode(ctx, nodeIP)
			eventCh := make(chan talosclient.EventResult, 10)

			watchCtx, watchCancel := context.WithTimeout(nodeCtx, 10*time.Second)

			go func() {
				// Note: Do NOT close eventCh - SDK may still send after this returns
				c.talos.EventsWatchV2(watchCtx, eventCh, talosclient.WithTailEvents(10))
			}()

			// Check recent events for machine status (with timeout)
		eventLoop:
			for {
				select {
				case <-watchCtx.Done():
					break eventLoop
				case result := <-eventCh:
					if result.Error != nil {
						break eventLoop
					}

					if progress := c.parseEvent(result.Event); progress != nil {
						if onProgress != nil {
							onProgress(*progress)
						}
						if progress.Done {
							watchCancel()
							// Also verify k8s node is ready
							if c.k8s == nil || c.isK8sNodeReady(ctx, nodeIP) {
								return nil
							}
						}
					}
				}
			}
			watchCancel()

			// If no RUNNING event but node is reachable and k8s ready, we're done
			if c.k8s == nil || c.isK8sNodeReady(ctx, nodeIP) {
				if onProgress != nil {
					onProgress(UpgradeProgress{Stage: "running", Done: true})
				}
				return nil
			}
		}
	}

	return fmt.Errorf("timeout waiting for node %s to come back after reboot", nodeIP)
}
