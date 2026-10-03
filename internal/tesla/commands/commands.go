package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/teslamotors/vehicle-command/pkg/protocol"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/keys"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
)

// legacyRouteCommands are accepted on the command route since wimaha 2.3.0 without being
// Fleet vehicle commands of the registry (fleetVehicleCommands.go): (*Command).Send handles
// them in its switch and they take no validated body.
var legacyRouteCommands = []string{"vehicle_data", "session_info"}
var ExceptedEndpoints = []string{"charge_state", "climate_state", "drive_state"}

// VehicleDataEndpointNames returns the sorted names of the vehicle_data endpoints served by the
// proxy. The slice is a fresh copy.
func VehicleDataEndpointNames() []string {
	names := slices.Clone(ExceptedEndpoints)
	slices.Sort(names)
	return names
}

// convertVehicleData converts the BLE vehicle data of an endpoint to its API model.
// It reports false (and returns nil) for an endpoint without converter.
func convertVehicleData(endpoint string, data *carserver.VehicleData) (interface{}, bool) {
	switch endpoint {
	case "charge_state":
		return models.ChargeStateFromBle(data), true
	case "climate_state":
		return models.ClimateStateFromBle(data), true
	case "drive_state":
		return models.DriveStateFromBle(data), true
	}
	return nil, false
}

// BodyControllerStateJSON is the response of the body_controller_state route (snake_case since 2.1.1).
func BodyControllerStateJSON(vs *vcsec.VehicleStatus) (json.RawMessage, error) {
	return json.Marshal(models.VehicleStatusFromBle(vs))
}

func (command *Command) Send(ctx context.Context, car *vehicle.Vehicle) (shouldRetry bool, err error) {
	if handler, ok := fleetVehicleCommands[command.Command]; ok {
		return handler.run(ctx, car, command.Body)
	}

	switch command.Command {
	case "session_info":
		// Get active key files
		_, publicKeyFile := config.GetActiveKeyFiles()
		publicKey, err := protocol.LoadPublicKey(publicKeyFile)
		if err != nil {
			return false, fmt.Errorf("failed to load public key: %s", err)
		}

		info, err := car.SessionInfo(ctx, publicKey, protocol.DomainVCSEC)
		if err != nil {
			return true, fmt.Errorf("failed session_info: %s", err)
		}
		fmt.Printf("%s\n", info)
	case "add-key-request":
		// Get role from command body, default to charging_manager (recommended for security)
		roleStr := "charging_manager"
		if command.Body != nil {
			if role, ok := command.Body["role"].(string); ok && role != "" {
				roleStr = role
			}
		}

		// Validate role to prevent path traversal
		// Check against valid roles
		validRoles := []string{"owner", "charging_manager"}
		isValid := false
		for _, validRole := range validRoles {
			if roleStr == validRole {
				isValid = true
				break
			}
		}
		if !isValid {
			return false, fmt.Errorf("invalid role: %s. Valid roles are: owner, charging_manager", roleStr)
		}
		// Prevent path traversal attempts
		if strings.Contains(roleStr, "..") || strings.Contains(roleStr, "/") || strings.Contains(roleStr, "\\") {
			return false, fmt.Errorf("invalid role: contains path traversal characters")
		}

		// Get public key file for the specified role
		_, publicKeyFile := config.GetKeyFilesForRole(roleStr)
		publicKey, err := protocol.LoadPublicKey(publicKeyFile)
		if err != nil {
			return false, fmt.Errorf("failed to load public key: %s", err)
		}

		// Map role string to keys.Role enum
		var keyRole keys.Role
		switch roleStr {
		case "owner":
			keyRole = keys.Role_ROLE_OWNER
		case "charging_manager":
			keyRole = keys.Role_ROLE_CHARGING_MANAGER
		default:
			// Default to charging_manager (recommended for security)
			keyRole = keys.Role_ROLE_CHARGING_MANAGER
		}

		// Get display name for logging
		displayName := roleStr
		if roleStr == "" {
			displayName = "Legacy (Owner)"
		} else {
			switch roleStr {
			case "owner":
				displayName = "Owner"
			case "charging_manager":
				displayName = "Charging Manager"
			}
		}

		if err := car.SendAddKeyRequestWithRole(ctx, publicKey, keyRole, vcsec.KeyFormFactor_KEY_FORM_FACTOR_CLOUD_KEY); err != nil {
			return true, fmt.Errorf("failed to add key: %s", err)
		} else {
			logging.Info(fmt.Sprintf("Sent add-key request to %s with role %s. Confirm by tapping NFC card on center console.", car.VIN(), displayName))
		}
	case "vehicle_data":
		if command.Body == nil {
			return false, fmt.Errorf("request body is nil")
		}

		endpoints, ok := command.Body["endpoints"].([]string)
		if !ok {
			return false, fmt.Errorf("missing or invalid 'endpoints' in request body")
		}

		response := make(map[string]json.RawMessage)
		for _, endpoint := range endpoints {
			//log.Debugf("get: %s", endpoint)
			category, err := GetCategory(endpoint)
			if err != nil {
				return false, err
			}
			data, err := car.GetState(ctx, category)
			if err != nil {
				return true, fmt.Errorf("Failed to get vehicle data: %s", err)
			}
			/*d, err := protojson.Marshal(data)
			if err != nil {
				return true, fmt.Errorf("failed to marshal vehicle data: %s", err)
			}
			logging.Debugf("data: %s", d)*/

			converted, _ := convertVehicleData(endpoint, data) // nil (JSON null) without converter, as in 2.3.0
			d, err := json.Marshal(converted)
			if err != nil {
				return true, fmt.Errorf("Failed to marshal vehicle data: %s", err)
			}

			response[endpoint] = d
		}

		responseJson, err := json.Marshal(response)
		if err != nil {
			return false, fmt.Errorf("failed to marshal vehicle data: %s", err)
		}
		command.Response.Response = responseJson
	case "body-controller-state":
		vs, err := car.BodyControllerState(ctx)
		if err != nil {
			return true, fmt.Errorf("failed to get body controller state: %s", err)
		}
		vsJson, err := BodyControllerStateJSON(vs)
		if err != nil {
			return true, fmt.Errorf("failed to marshal body-controller-state: %s", err)
		}
		if command.Response != nil {
			command.Response.Response = vsJson
		}
	default:
		return false, fmt.Errorf("unrecognized command: %s", command.Command)
	}

	// everything fine
	return false, nil
}
