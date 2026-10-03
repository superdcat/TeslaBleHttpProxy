package commands

import (
	"fmt"
	"strings"

	"github.com/teslamotors/vehicle-command/pkg/vehicle"
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

// CommandDomain returns the domain a queued command needs: VCSEC for body_controller_state, which
// never wakes the vehicle; None for the others, whose connection starts the infotainment session
// and wakes the vehicle as in wimaha 2.3.0.
// Adapted from Lenart12/TeslaBleHttpProxy (Command.Domain, commit 94d1fd8).
func CommandDomain(command string) DomainType {
	if command == BodyControllerStateCommand {
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

// 'charge_state', 'climate_state', 'closures_state', 'drive_state', 'gui_settings', 'location_data', 'charge_schedule_data', 'preconditioning_schedule_data', 'vehicle_config', 'vehicle_state', 'vehicle_data_combo'
var categoriesByName = map[string]vehicle.StateCategory{
	"charge_state":          vehicle.StateCategoryCharge,
	"climate_state":         vehicle.StateCategoryClimate,
	"drive":                 vehicle.StateCategoryDrive,
	"drive_state":           vehicle.StateCategoryDrive,
	"closures_state":        vehicle.StateCategoryClosures,
	"charge-schedule":       vehicle.StateCategoryChargeSchedule,
	"precondition-schedule": vehicle.StateCategoryPreconditioningSchedule,
	"tire-pressure":         vehicle.StateCategoryTirePressure,
	"media":                 vehicle.StateCategoryMedia,
	"media-detail":          vehicle.StateCategoryMediaDetail,
	"software-update":       vehicle.StateCategorySoftwareUpdate,
	"parental-controls":     vehicle.StateCategoryParentalControls,
}

func GetCategory(nameStr string) (vehicle.StateCategory, error) {
	if category, ok := categoriesByName[strings.ToLower(nameStr)]; ok {
		return category, nil
	}
	return 0, fmt.Errorf("unrecognized state category '%s'", nameStr)
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
