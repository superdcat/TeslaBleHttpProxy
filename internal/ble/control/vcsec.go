// Reads of the body controller state (VCSEC domain) served by the BLE queue.
//
// Adapted from Lenart12/TeslaBleHttpProxy (body_controller_state processed by the command queue
// in the VCSEC domain, commit 94d1fd8), Copyright Lenart12 and contributors, Apache License 2.0.
// Rewritten in the superdcat fork for the queue of wimaha 2.3.0 (no copy of Lenart12's control.go).

package control

import "github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"

// vcsecOnlyConnection reports whether the connection opened for firstCommand only starts the
// VCSEC session: no wake-up, no infotainment session (same condition as TryConnectToVehicle;
// wake_up is excluded because operateConnection upgrades its session).
func vcsecOnlyConnection(firstCommand *commands.Command) bool {
	return firstCommand.Domain == commands.Domain.VCSEC && firstCommand.Command != "wake_up"
}

// connectionAttempts is the number of connection attempts for firstCommand. A VCSEC only read
// makes a single attempt, as the 2.3.0 handler did within its 15 s: a retry would replace
// "Vehicle is not in range" by a deadline error.
func connectionAttempts(firstCommand *commands.Command) int {
	if vcsecOnlyConnection(firstCommand) {
		return 1
	}
	return 3
}
