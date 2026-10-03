// Registry of the Fleet API vehicle commands served on
// POST /api/1/vehicles/{vin}/command/{command}: each command is described once
// (body validation, execution, interpretation of the vehicle error).
//
// Derived from Lenart12/TeslaBleHttpProxy, internal/tesla/commands/fleetVehicleCommands.go
// (commit 94d1fd8), Copyright Lenart12 and contributors, Apache License 2.0.
// Modified in the superdcat fork: lenient parsing of wimaha 2.3.0 (numeric strings,
// "true"/"false", decimals truncated), no value bounds, wimaha 2.3.0 error messages,
// validation without mutating the body, vehicle behind an interface for tests.

package commands

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
)

// ErrInvalidBody prefixes every request body error: "invalid request body: <detail>".
// Only the prefix is stable; the detail never echoes the client's values.
var ErrInvalidBody = errors.New("invalid request body")

// vehicleCommander lists the SDK methods used by the registry.
type vehicleCommander interface {
	ClimateOn(ctx context.Context) error
	ClimateOff(ctx context.Context) error
	ChargePortOpen(ctx context.Context) error
	ChargePortClose(ctx context.Context) error
	FlashLights(ctx context.Context) error
	Wakeup(ctx context.Context) error
	HonkHorn(ctx context.Context) error
	Lock(ctx context.Context) error
	Unlock(ctx context.Context) error
	SetSentryMode(ctx context.Context, state bool) error
	ChargeStart(ctx context.Context) error
	ChargeStop(ctx context.Context) error
	SetChargingAmps(ctx context.Context, amps int32) error
	ChangeChargeLimit(ctx context.Context, chargeLimitPercent int32) error
}

var _ vehicleCommander = (*vehicle.Vehicle)(nil)

// commandArgs is the decoded JSON body of a command (nil when absent or unreadable).
type commandArgs map[string]interface{}

type commandHandler struct {
	// validate checks the body before the command is queued; it must not modify args.
	// nil: the command takes no body and any body is ignored, as in wimaha 2.3.0.
	validate func(args commandArgs) error
	// execute sends the command to the vehicle and wraps its error with the 2.3.0 message (%w).
	// It is called only by run, after validate succeeded; it may re-read its arguments and
	// ignore their parse errors.
	execute func(ctx context.Context, car vehicleCommander, args commandArgs) error
	// checkError turns a vehicle error meaning "already done" into success (nil). Optional.
	checkError func(err error) error
}

var fleetVehicleCommands = map[string]commandHandler{
	"auto_conditioning_start": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.ClimateOn(ctx); err != nil {
				return fmt.Errorf("failed to start auto conditioning: %w", err)
			}
			return nil
		},
	},
	"auto_conditioning_stop": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.ClimateOff(ctx); err != nil {
				return fmt.Errorf("failed to stop auto conditioning: %w", err)
			}
			return nil
		},
	},
	"charge_port_door_open": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.ChargePortOpen(ctx); err != nil {
				return fmt.Errorf("failed to open charge port: %w", err)
			}
			return nil
		},
	},
	"charge_port_door_close": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.ChargePortClose(ctx); err != nil {
				return fmt.Errorf("failed to close charge port: %w", err)
			}
			return nil
		},
	},
	"flash_lights": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.FlashLights(ctx); err != nil {
				return fmt.Errorf("failed to flash lights: %w", err)
			}
			return nil
		},
	},
	"wake_up": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.Wakeup(ctx); err != nil {
				return fmt.Errorf("failed to wake up car: %w", err)
			}
			return nil
		},
	},
	"honk_horn": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.HonkHorn(ctx); err != nil {
				return fmt.Errorf("failed to honk horn %w", err)
			}
			return nil
		},
	},
	"door_lock": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.Lock(ctx); err != nil {
				return fmt.Errorf("failed to lock %w", err)
			}
			return nil
		},
	},
	"door_unlock": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.Unlock(ctx); err != nil {
				return fmt.Errorf("failed to unlock %w", err)
			}
			return nil
		},
	},
	"set_sentry_mode": {
		validate: func(args commandArgs) error {
			_, err := args.boolArg("on")
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			on, _ := args.boolArg("on") // validated by run
			if err := car.SetSentryMode(ctx, on); err != nil {
				return fmt.Errorf("failed to set sentry mode %w", err)
			}
			return nil
		},
	},
	"charge_start": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.ChargeStart(ctx); err != nil {
				return fmt.Errorf("failed to start charge: %w", err)
			}
			return nil
		},
		checkError: func(err error) error {
			if strings.Contains(err.Error(), "is_charging") {
				// The car is already charging, so the command is somehow successfully executed.
				logging.Info("The car is already charging")
				return nil
			}
			if strings.Contains(err.Error(), "complete") {
				// The charging is completed, so the command is somehow successfully executed.
				logging.Info("The charging is completed")
				return nil
			}
			return err
		},
	},
	"charge_stop": {
		execute: func(ctx context.Context, car vehicleCommander, _ commandArgs) error {
			if err := car.ChargeStop(ctx); err != nil {
				return fmt.Errorf("failed to stop charge: %w", err)
			}
			return nil
		},
		checkError: func(err error) error {
			if strings.Contains(err.Error(), "not_charging") {
				// The car has already stopped charging, so the command is somehow successfully executed.
				logging.Info("The car has already stopped charging")
				return nil
			}
			return err
		},
	},
	"set_charging_amps": {
		validate: func(args commandArgs) error {
			_, err := args.int32Arg("charging_amps")
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			chargingAmps, _ := args.int32Arg("charging_amps") // validated by run
			if err := car.SetChargingAmps(ctx, chargingAmps); err != nil {
				return fmt.Errorf("failed to set charging Amps to %d: %w", chargingAmps, err)
			}
			return nil
		},
	},
	"set_charge_limit": {
		validate: func(args commandArgs) error {
			_, err := args.int32Arg("percent")
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			chargeLimit, _ := args.int32Arg("percent") // validated by run
			if err := car.ChangeChargeLimit(ctx, chargeLimit); err != nil {
				return fmt.Errorf("failed to set charge limit to %d %%: %w", chargeLimit, err)
			}
			return nil
		},
	},
}

// IsSupportedCommand reports whether name is accepted on the command route.
func IsSupportedCommand(name string) bool {
	if _, ok := fleetVehicleCommands[name]; ok {
		return true
	}
	return slices.Contains(legacyRouteCommands, name)
}

// FleetCommandNames returns the sorted names of the Fleet vehicle commands of the registry
// (the legacy route commands are not included). The slice is a fresh copy.
func FleetCommandNames() []string {
	names := make([]string, 0, len(fleetVehicleCommands))
	for name := range fleetVehicleCommands {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// ValidateCommandBody checks the body of a registry command before it is queued.
// Commands without body and commands outside the registry always pass.
func ValidateCommandBody(name string, body map[string]interface{}) error {
	handler, ok := fleetVehicleCommands[name]
	if !ok || handler.validate == nil {
		return nil
	}
	return handler.validate(commandArgs(body))
}

// run validates the body again (internal callers may queue commands), executes the command and
// applies checkError. A body error is not retried; a vehicle error is retried, as in 2.3.0.
func (handler commandHandler) run(ctx context.Context, car vehicleCommander, body map[string]interface{}) (shouldRetry bool, err error) {
	args := commandArgs(body)
	if handler.validate != nil {
		if err := handler.validate(args); err != nil {
			return false, err
		}
	}
	if err := handler.execute(ctx, car, args); err != nil {
		if handler.checkError != nil {
			err = handler.checkError(err)
		}
		if err != nil {
			return true, err
		}
	}
	return false, nil
}

func invalidBodyf(format string, a ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalidBody, fmt.Sprintf(format, a...))
}

// int32Arg reads the required integer key with the tolerance of wimaha 2.3.0: a JSON number
// (decimals truncated toward zero) or a decimal string (strconv.ParseInt). No bound other
// than the int32 range: the vehicle refuses the values it does not support.
func (args commandArgs) int32Arg(key string) (int32, error) {
	switch v := args[key].(type) {
	case nil:
		return 0, invalidBodyf("%s missing", key)
	case float64:
		truncated := math.Trunc(v)
		if truncated < math.MinInt32 || truncated > math.MaxInt32 {
			return 0, invalidBodyf("%s is out of range", key)
		}
		return int32(truncated), nil
	case string:
		n, err := strconv.ParseInt(v, 10, 32)
		if errors.Is(err, strconv.ErrRange) {
			return 0, invalidBodyf("%s is out of range", key)
		}
		if err != nil {
			return 0, invalidBodyf("%s is not a valid integer", key)
		}
		return int32(n), nil
	default:
		return 0, invalidBodyf("%s must be a number or a numeric string", key)
	}
}

// boolArg reads the required boolean key with the tolerance of wimaha 2.3.0: a JSON boolean
// or a string accepted by strconv.ParseBool ("true", "false", "1", "0", ...).
func (args commandArgs) boolArg(key string) (bool, error) {
	switch v := args[key].(type) {
	case nil:
		return false, invalidBodyf("%s missing", key)
	case bool:
		return v, nil
	case string:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return false, invalidBodyf("%s is not a valid boolean", key)
		}
		return b, nil
	default:
		return false, invalidBodyf("%s must be a boolean or \"true\"/\"false\"", key)
	}
}
