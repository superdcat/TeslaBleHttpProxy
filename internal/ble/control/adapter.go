// Choice of the Bluetooth adapter, connection hold and release of the adapter while idle.
//
// btAdapter is adapted from Lenart12/TeslaBleHttpProxy (commit feadd14), Copyright Lenart12 and
// contributors, Apache License 2.0: validated, and opened at startup only when set. The
// connection hold and the release of the adapter are inspired by the unmerged
// wimaha/TeslaBleHttpProxy pull request 161 (AloisKlingler), reimplemented (no code is copied),
// with the defaults of 2.3.0 kept: both are off unless their variable is set.

package control

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
)

// Boundaries of the SDK and the clock, replaced in tests (no adapter, no waiting).
var (
	initAdapterWithID  = ble.InitAdapterWithID
	closeAdapter       = ble.CloseAdapter
	connectionHoldUnit = time.Second
)

// Sharing guard. The SDK keeps one adapter and reopens the default one after CloseAdapter, so
// the adapter is only closed when nobody uses it, and every scan runs between beginAdapterUse
// and endAdapterUse. State is protected by adapterMu.
var (
	adapterMu    sync.Mutex
	adapterUsers int  // users in progress: the queue during a command, SendKeysToVehicle
	adapterOpen  bool // opened by acquireAdapter or SetupAdapter and not given back yet
)

// adapterOptionsActive reports whether btAdapter or releaseAdapterWhenIdle is set. Without
// either, the SDK opens the default adapter by itself, as in 2.3.0.
func adapterOptionsActive() bool {
	return config.AppConfig != nil && (config.AppConfig.BTAdapter != "" || config.AppConfig.ReleaseAdapterWhenIdle)
}

// SetupAdapter validates btAdapter and opens that adapter at startup, so that an unknown adapter
// stops the proxy at once. It does nothing when btAdapter is empty.
func SetupAdapter() error {
	if config.AppConfig == nil || config.AppConfig.BTAdapter == "" {
		return nil
	}
	id, err := config.ParseBTAdapter(config.AppConfig.BTAdapter)
	if err != nil {
		return err
	}
	adapterMu.Lock()
	defer adapterMu.Unlock()
	if err := initAdapterWithID(id); err != nil {
		hint := ""
		if strings.Contains(err.Error(), "operation not permitted") {
			hint = fmt.Sprintf("; grant this application CAP_NET_ADMIN: sudo setcap 'cap_net_admin=eip' \"$(which %s)\"", os.Args[0])
		}
		return fmt.Errorf("Bluetooth adapter %q (btAdapter) cannot be opened: %w; check its name with \"btmgmt info\" or \"hciconfig -a\", or leave btAdapter empty to use the default adapter%s", id, err, hint)
	}
	adapterOpen = true
	logging.Info("Bluetooth adapter opened", "btAdapter", id)
	// Nobody uses it yet: give it back until the first command needs it.
	releaseIfUnused()
	return nil
}

// acquireAdapter (re)opens the configured adapter before a scan. It does nothing, and calls no
// SDK function, when neither option is set. Call it inside a beginAdapterUse window.
func acquireAdapter() error {
	if !adapterOptionsActive() {
		return nil
	}
	adapterMu.Lock()
	defer adapterMu.Unlock()
	if err := initAdapterWithID(config.AppConfig.BTAdapter); err != nil {
		return err
	}
	adapterOpen = true
	return nil
}

// beginAdapterUse marks the adapter as in use: it is not closed until the matching endAdapterUse.
func beginAdapterUse() {
	adapterMu.Lock()
	adapterUsers++
	adapterMu.Unlock()
}

// endAdapterUse ends a use of the adapter. When it was the last one, mayRelease is true and the
// release is enabled, the adapter is given back to the system.
func endAdapterUse(mayRelease bool) {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	if adapterUsers > 0 {
		adapterUsers--
	}
	if mayRelease {
		releaseIfUnused()
	}
}

// releaseIfUnused closes the adapter when releaseAdapterWhenIdle is set, it is open and nobody
// uses it. The caller holds adapterMu.
func releaseIfUnused() {
	if adapterUsers != 0 || !adapterOpen || config.AppConfig == nil || !config.AppConfig.ReleaseAdapterWhenIdle {
		return
	}
	logging.Debug("Releasing BLE adapter while idle (releaseAdapterWhenIdle)")
	adapterOpen = false
	if err := closeAdapter(); err != nil {
		logging.Warn("Failed to release the BLE adapter", "error", err)
	}
}

// defaultConnectionHold is the hold of 2.3.0.
func defaultConnectionHold() time.Duration {
	return config.DefaultConnectionTimeout * connectionHoldUnit
}

// connectionHold is how long a connection stays open, counted from its opening.
func connectionHold() time.Duration {
	if config.AppConfig == nil || config.AppConfig.ConnectionTimeout <= 0 {
		return defaultConnectionHold()
	}
	return time.Duration(config.AppConfig.ConnectionTimeout) * connectionHoldUnit
}

// newConnectionContexts returns the context of the connection hold and the one that bounds the
// first command. With a hold shorter than firstBudget the first command keeps firstBudget, so a
// slow command is not cut and handed back again and again; otherwise both are the same context.
func newConnectionContexts(hold, firstBudget time.Duration) (connectionCtx, firstCommandCtx context.Context, cancel context.CancelFunc) {
	connectionCtx, cancelConnection := context.WithTimeout(context.Background(), hold)
	if hold >= firstBudget {
		return connectionCtx, connectionCtx, cancelConnection
	}
	firstCommandCtx, cancelFirst := context.WithTimeout(context.Background(), firstBudget)
	return connectionCtx, firstCommandCtx, func() {
		cancelFirst()
		cancelConnection()
	}
}
