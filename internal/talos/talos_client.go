package talos

import (
	"context"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// TalosMachineClient abstracts Talos SDK operations for testing.
// This interface allows mocking the Talos SDK client in unit tests.
type TalosMachineClient interface {
	// Close closes the client connection
	Close() error

	// Version retrieves the Talos version
	Version(ctx context.Context) (*machine.VersionResponse, error)

	// UpgradeWithOptions performs an upgrade with the given options
	UpgradeWithOptions(ctx context.Context, opts ...talosclient.UpgradeOption) (*machine.UpgradeResponse, error)

	// ServiceInfo retrieves information about a specific service
	ServiceInfo(ctx context.Context, service string) ([]talosclient.ServiceInfo, error)

	// EventsWatchV2 watches for machine events
	EventsWatchV2(ctx context.Context, eventCh chan<- talosclient.EventResult, opts ...talosclient.EventsOptionFunc) error

	// COSIList lists COSI resources
	COSIList(ctx context.Context, md resource.Metadata) (resource.List, error)
}

// talosClientWrapper wraps the real SDK client to implement TalosMachineClient
type talosClientWrapper struct {
	client *talosclient.Client
}

// newTalosClientWrapper creates a wrapper around the SDK client
func newTalosClientWrapper(client *talosclient.Client) *talosClientWrapper {
	return &talosClientWrapper{client: client}
}

func (w *talosClientWrapper) Close() error {
	return w.client.Close()
}

func (w *talosClientWrapper) Version(ctx context.Context) (*machine.VersionResponse, error) {
	return w.client.Version(ctx)
}

func (w *talosClientWrapper) UpgradeWithOptions(ctx context.Context, opts ...talosclient.UpgradeOption) (*machine.UpgradeResponse, error) {
	return w.client.UpgradeWithOptions(ctx, opts...)
}

func (w *talosClientWrapper) ServiceInfo(ctx context.Context, service string) ([]talosclient.ServiceInfo, error) {
	return w.client.ServiceInfo(ctx, service)
}

func (w *talosClientWrapper) EventsWatchV2(ctx context.Context, eventCh chan<- talosclient.EventResult, opts ...talosclient.EventsOptionFunc) error {
	return w.client.EventsWatchV2(ctx, eventCh, opts...)
}

func (w *talosClientWrapper) COSIList(ctx context.Context, md resource.Metadata) (resource.List, error) {
	return w.client.COSI.List(ctx, md)
}

// Ensure talosClientWrapper implements TalosMachineClient
var _ TalosMachineClient = (*talosClientWrapper)(nil)

// COSIState wraps the COSI state client for advanced operations
type COSIState interface {
	state.CoreState
}
