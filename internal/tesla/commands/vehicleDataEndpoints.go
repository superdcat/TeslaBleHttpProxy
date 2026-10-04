// Registry of the vehicle_data endpoints, written for the superdcat fork. It replaces the
// ExceptedEndpoints list and the categoriesByName map of wimaha 2.3.0 (whose hyphenated names
// were never served over HTTP). The drive_state endpoint comes from wimaha PR #160 (Optic00).
// The closures_state name and category are those of categoriesByName of wimaha 2.3.0 (never
// served); its typed model is written for the fork. The tire_pressure and software_update names
// are those of Lenart12 94d1fd8 (wimaha 2.3.0 had tire-pressure and software-update, never
// served); their typed models are written for the fork. The location_data name is that of the
// Fleet API and of Lenart12 94d1fd8 (absent from wimaha 2.3.0); its typed model, a subset of
// LocationState, is written for the fork. The charge_schedule_data and preconditioning_schedule_data
// names are those of the Fleet API and of Lenart12 94d1fd8 (wimaha 2.3.0 had charge-schedule and
// precondition-schedule, never served); their typed models are written for the fork.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
)

// vehicleDataEndpoint describes one endpoint of GET /api/1/vehicles/{vin}/vehicle_data.
type vehicleDataEndpoint struct {
	// category is passed to (*vehicle.Vehicle).GetState.
	category vehicle.StateCategory
	// convert builds the API model from the BLE data; every field is present (zero values).
	convert func(*carserver.VehicleData) any
}

var vehicleDataEndpoints = map[string]vehicleDataEndpoint{
	"charge_state": {vehicle.StateCategoryCharge, func(d *carserver.VehicleData) any {
		return models.ChargeStateFromBle(d)
	}},
	"climate_state": {vehicle.StateCategoryClimate, func(d *carserver.VehicleData) any {
		return models.ClimateStateFromBle(d)
	}},
	"drive_state": {vehicle.StateCategoryDrive, func(d *carserver.VehicleData) any {
		return models.DriveStateFromBle(d)
	}},
	"closures_state": {vehicle.StateCategoryClosures, func(d *carserver.VehicleData) any {
		return models.ClosuresStateFromBle(d)
	}},
	"tire_pressure": {vehicle.StateCategoryTirePressure, func(d *carserver.VehicleData) any {
		return models.TirePressureFromBle(d)
	}},
	"software_update": {vehicle.StateCategorySoftwareUpdate, func(d *carserver.VehicleData) any {
		return models.SoftwareUpdateFromBle(d)
	}},
	"location_data": {vehicle.StateCategoryLocation, func(d *carserver.VehicleData) any {
		return models.LocationDataFromBle(d)
	}},
	"charge_schedule_data": {vehicle.StateCategoryChargeSchedule, func(d *carserver.VehicleData) any {
		return models.ChargeScheduleDataFromBle(d)
	}},
	"preconditioning_schedule_data": {vehicle.StateCategoryPreconditioningSchedule, func(d *carserver.VehicleData) any {
		return models.PreconditioningScheduleDataFromBle(d)
	}},
}

// defaultVehicleDataEndpoints is served when the request names no endpoint (wimaha 2.3.0),
// and read in this order.
var defaultVehicleDataEndpoints = []string{"charge_state", "climate_state"}

// IsSupportedEndpoint reports whether name is a vehicle_data endpoint (exact, case-sensitive).
func IsSupportedEndpoint(name string) bool {
	_, ok := vehicleDataEndpoints[name]
	return ok
}

// VehicleDataEndpointNames returns the sorted names of the vehicle_data endpoints served by the
// proxy. The slice is a fresh copy and never nil.
func VehicleDataEndpointNames() []string {
	names := make([]string, 0, len(vehicleDataEndpoints))
	for name := range vehicleDataEndpoints {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// DefaultVehicleDataEndpoints returns the endpoints read when the request names none. The slice
// is a fresh copy (it goes into the queued command body).
func DefaultVehicleDataEndpoints() []string {
	return slices.Clone(defaultVehicleDataEndpoints)
}

// VehicleDataJSON reads each endpoint through read and returns the JSON object
// {"<endpoint>": <model>, ...}. The boolean tells whether the failure is worth a retry on a new
// connection. Error texts are those of wimaha 2.3.0.
func VehicleDataJSON(ctx context.Context, endpoints []string,
	read func(context.Context, vehicle.StateCategory) (*carserver.VehicleData, error)) (json.RawMessage, bool, error) {
	response := make(map[string]json.RawMessage)
	for _, name := range endpoints {
		endpoint, ok := vehicleDataEndpoints[name]
		if !ok {
			return nil, false, fmt.Errorf("unrecognized state category '%s'", name)
		}
		data, err := read(ctx, endpoint.category)
		if err != nil {
			return nil, true, fmt.Errorf("Failed to get vehicle data: %s", err)
		}
		d, err := json.Marshal(endpoint.convert(data))
		if err != nil {
			return nil, true, fmt.Errorf("Failed to marshal vehicle data: %s", err)
		}
		response[name] = d
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal vehicle data: %s", err)
	}
	return responseJSON, false, nil
}
