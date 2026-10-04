package commands

import (
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
)

type DomainType string

var Domain = struct {
	None         DomainType
	VCSEC        DomainType
	Infotainment DomainType
}{
	None:         "",
	VCSEC:        "vcsec",
	Infotainment: "infotainment",
}

// BodyControllerStateCommand is the queued command of GET /api/1/vehicles/{vin}/body_controller_state.
// It is not accepted on the command route (IsSupportedCommand).
const BodyControllerStateCommand = "body-controller-state"

// ConnectionStatusCommand is the queued command of GET /api/proxy/1/vehicles/{vin}/connection_status.
// The BLE queue serves it with a scan, without connecting; it is not accepted on the command route
// (IsSupportedCommand).
const ConnectionStatusCommand = "connection_status"

// CommandDomain returns the domain a queued command needs: VCSEC for body_controller_state, which
// never wakes the vehicle, and for connection_status, which opens no session at all (VCSEC only so
// that a connection could never wake the vehicle); None for the others, whose connection starts the
// infotainment session and wakes the vehicle as in wimaha 2.3.0.
// Adapted from Lenart12/TeslaBleHttpProxy (Command.Domain, commit 94d1fd8).
func CommandDomain(command string) DomainType {
	if command == BodyControllerStateCommand || command == ConnectionStatusCommand {
		return Domain.VCSEC
	}
	return Domain.None
}

type Command struct {
	Command    string
	Domain     DomainType
	Vin        string
	Body       map[string]interface{}
	Response   *models.ApiResponse
	AutoWakeup bool
	// SendAttempts counts the sendings handed to the SDK, also across retries on a new connection.
	SendAttempts int
}

// Abandoned returns the error of the context of the HTTP request waiting for the command
// (context.Canceled when the client hung up, context.DeadlineExceeded past its deadline), or nil
// while it waits. A command nobody waits for (wait=false, internal commands) is never abandoned.
// Adapted from Lenart12/TeslaBleHttpProxy (IsContextDone, commit 64390c6), without side effect.
func (command *Command) Abandoned() error {
	if command.Response == nil || command.Response.Ctx == nil {
		return nil
	}
	return command.Response.Ctx.Err()
}
