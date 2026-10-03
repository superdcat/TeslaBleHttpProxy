package handlers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
)

// TestCapabilitiesKeyRoleFollowsActiveKey changes the working directory (the key files are
// read relative to it): it is the only test of the repository to do so.
func TestCapabilitiesKeyRoleFollowsActiveKey(t *testing.T) {
	// TempDir before Chdir: cleanups run last-in first-out, and Windows cannot delete the
	// current directory.
	dir := t.TempDir()
	t.Chdir(dir)

	previous := control.BleControlInstance
	t.Cleanup(func() { control.BleControlInstance = previous })
	control.BleControlInstance = nil // key_role must not depend on the BLE state

	check := func(step, want string) {
		t.Helper()
		if got := activeKeyRole(); got != want {
			t.Errorf("%s: key_role %q, want %q", step, got, want)
		}
	}

	check("no key", "")

	if err := control.CreatePrivateAndPublicKeyFileForRole("charging_manager"); err != nil {
		t.Fatalf("create charging_manager key: %v", err)
	}
	check("charging_manager key, no active_key.json", "charging_manager")

	if err := control.CreatePrivateAndPublicKeyFileForRole("owner"); err != nil {
		t.Fatalf("create owner key: %v", err)
	}
	check("owner key installed but not activated", "charging_manager")

	if err := control.SetActiveKeyRole("owner"); err != nil {
		t.Fatalf("activate owner: %v", err)
	}
	check("owner activated", "owner")

	if err := os.RemoveAll(filepath.Join("key", "owner")); err != nil {
		t.Fatalf("remove owner key: %v", err)
	}
	check("active owner key removed", "")

	if err := os.WriteFile(filepath.Join("key", "active_key.json"), []byte(`{"role":"admin"}`), 0o644); err != nil {
		t.Fatalf("write active_key.json: %v", err)
	}
	check("unknown role", "")
}
