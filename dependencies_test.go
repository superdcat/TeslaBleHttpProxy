package main

import (
	"fmt"
	"runtime/debug"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/universalmessage"
	"github.com/wimaha/TeslaBleHttpProxy/config"
)

// linkedModule returns the module of the test binary's build information with the given path.
func linkedModule(t *testing.T, path string) *debug.Module {
	t.Helper()
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("build information not available")
	}
	for _, dep := range info.Deps {
		if dep != nil && dep.Path == path {
			return dep
		}
	}
	t.Fatalf("module %s is not linked into the binary", path)
	return nil
}

// The proxy links Tesla's official SDK directly, without the wimaha/vehicle-command replace.
// A local development replace of the SDK makes this guard fail on purpose: do not commit it.
func TestLinkedVehicleCommandSDKIsOfficial(t *testing.T) {
	dep := linkedModule(t, config.VehicleCommandModule)
	if dep.Replace != nil {
		t.Fatalf("%s is replaced by %s %s; go.mod must require the official module", dep.Path, dep.Replace.Path, dep.Replace.Version)
	}
	if got := config.SDKVersion(); got != dep.Version {
		t.Errorf("config.SDKVersion() = %q, want %q", got, dep.Version)
	}
}

// wimaha's BLE connection fix must stay in place.
func TestLinkedBLELibraryKeepsWimahaConnectFix(t *testing.T) {
	dep := linkedModule(t, "github.com/go-ble/ble")
	if dep.Replace == nil || dep.Replace.Path != "github.com/wimaha/ble_BleConnectFix" {
		t.Fatalf("github.com/go-ble/ble must be replaced by github.com/wimaha/ble_BleConnectFix, got %+v", dep.Replace)
	}
}

// Version guard, it does not test the proxy: the linked SDK must include teslamotors/vehicle-command
// a4b43c1 "Fix wrapped protocol error classification" (not in v0.4.1). Every case below fails
// with wimaha/vehicle-command v0.0.7, except the plain error.
func TestSDKVersionClassifiesWrappedErrors(t *testing.T) {
	tests := []struct {
		name             string
		err              error
		mayHaveSucceeded bool
		temporary        bool
		shouldRetry      bool
	}{
		{"busy", fmt.Errorf("failed to start charge: %w", protocol.ErrBusy), false, true, true},
		{"busy fault", fmt.Errorf("failed to lock: %w", &protocol.RoutableMessageError{Code: universalmessage.MessageFault_E_MESSAGEFAULT_ERROR_BUSY}), false, true, true},
		{"outcome unknown", fmt.Errorf("failed to unlock: %w", protocol.NewError("command outcome unknown", true, true)), true, true, false},
		{"double wrap", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", protocol.ErrBusy)), false, true, true},
		{"plain", fmt.Errorf("ble: failed to scan"), false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protocol.MayHaveSucceeded(tt.err); got != tt.mayHaveSucceeded {
				t.Errorf("MayHaveSucceeded = %v, want %v", got, tt.mayHaveSucceeded)
			}
			if got := protocol.Temporary(tt.err); got != tt.temporary {
				t.Errorf("Temporary = %v, want %v", got, tt.temporary)
			}
			if got := protocol.ShouldRetry(tt.err); got != tt.shouldRetry {
				t.Errorf("ShouldRetry = %v, want %v", got, tt.shouldRetry)
			}
		})
	}
}
