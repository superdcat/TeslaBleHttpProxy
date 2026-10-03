// Registry of the Fleet API vehicle commands served on
// POST /api/1/vehicles/{vin}/command/{command}: each command is described once
// (body validation, execution, interpretation of the vehicle error).
//
// Derived from Lenart12/TeslaBleHttpProxy, internal/tesla/commands/fleetVehicleCommands.go
// (commit 94d1fd8), Copyright Lenart12 and contributors, Apache License 2.0.
// Modified in the superdcat fork: lenient parsing of wimaha 2.3.0 (numeric strings,
// "true"/"false", decimals truncated), no value bounds on the wimaha 2.3.0 commands,
// wimaha 2.3.0 error messages, validation without mutating the body, explicit Fleet seat
// tables (Lenart12 shifted the seat heater by one), vehicle behind an interface for tests.
// set_temps follows the contract of wimaha/TeslaBleHttpProxy PR #162; actuate_trunk follows
// the contract of wimaha PR #162 (rear not retried).

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
	ChangeClimateTemp(ctx context.Context, driverCelsius float32, passengerCelsius float32) error
	SetPreconditioningMax(ctx context.Context, enabled bool, manualOverride bool) error
	SetClimateKeeperMode(ctx context.Context, mode vehicle.ClimateKeeperMode, override bool) error
	SetCabinOverheatProtection(ctx context.Context, enabled bool, fanOnly bool) error
	SetCabinOverheatProtectionTemperature(ctx context.Context, level vehicle.Level) error
	SetBioweaponDefenseMode(ctx context.Context, enabled bool, manualOverride bool) error
	SetSeatHeater(ctx context.Context, levels map[vehicle.SeatPosition]vehicle.Level) error
	SetSeatCooler(ctx context.Context, level vehicle.Level, seat vehicle.SeatPosition) error
	AutoSeatAndClimate(ctx context.Context, positions []vehicle.SeatPosition, enabled bool) error
	SetSteeringWheelHeater(ctx context.Context, enabled bool) error
	OpenFrunk(ctx context.Context) error
	ActuateTrunk(ctx context.Context) error
	VentWindows(ctx context.Context) error
	CloseWindows(ctx context.Context) error
}

// Bounds of the cabin temperature setpoints, in degrees Celsius, inclusive (wimaha PR #162).
const (
	minCabinTempCelsius = 15
	maxCabinTempCelsius = 28
)

// climateKeeperModes maps the Fleet climate_keeper_mode (the index) to the SDK mode:
// 0 off, 1 on, 2 dog, 3 camp.
var climateKeeperModes = [...]vehicle.ClimateKeeperMode{
	vehicle.ClimateKeeperModeOff,
	vehicle.ClimateKeeperModeOn,
	vehicle.ClimateKeeperModeDog,
	vehicle.ClimateKeeperModeCamp,
}

// copActivationLevels maps the Fleet cop_temp (the index) to the SDK level: 0 low (30 C),
// 1 medium (35 C), 2 high (40 C). vehicle.Level is shifted by one (LevelOff is 0), so a direct
// cast of cop_temp would be wrong. Source of the Fleet semantics:
// https://developer.tesla.com/docs/fleet-api/endpoints/vehicle-commands (set_cop_temp).
var copActivationLevels = [...]vehicle.Level{vehicle.LevelLow, vehicle.LevelMed, vehicle.LevelHigh}

// seatHeaterPositions maps the Fleet heater (the index) to the SDK seat. The SDK numbers its
// seats from SeatUnknown = 0, so vehicle.SeatPosition(heater) would be shifted by one seat.
// Same order as pkg/proxy/command.go seatPositions of the official proxy.
var seatHeaterPositions = [...]vehicle.SeatPosition{
	vehicle.SeatFrontLeft,
	vehicle.SeatFrontRight,
	vehicle.SeatSecondRowLeft,
	vehicle.SeatSecondRowLeftBack,
	vehicle.SeatSecondRowCenter,
	vehicle.SeatSecondRowRight,
	vehicle.SeatSecondRowRightBack,
	vehicle.SeatThirdRowLeft,
	vehicle.SeatThirdRowRight,
}

// seatLevels maps the Fleet level and seat_cooler_level (the index) to the SDK level: 0 off,
// 1 low, 2 medium, 3 high. SetSeatCooler adds one itself, so no "-1" here (vehicle-command #50).
var seatLevels = [...]vehicle.Level{vehicle.LevelOff, vehicle.LevelLow, vehicle.LevelMed, vehicle.LevelHigh}

// frontSeats maps the Fleet seat_position and auto_seat_position (the index, equal to the
// protobuf values) to the SDK seat: 1 front left, 2 front right. Index 0 is never read.
var frontSeats = [...]vehicle.SeatPosition{1: vehicle.SeatFrontLeft, 2: vehicle.SeatFrontRight}

// firstFrontSeat is the lowest valid index of frontSeats.
const firstFrontSeat = 1

// climateKeeperManualOverride is always sent by set_climate_keeper_mode, as in Lenart12
// (commit 94d1fd8); a manual_override key in the body is not read.
const climateKeeperManualOverride = true

// Choices of actuate_trunk (which_trunk) and window_control (command), in the order of the messages.
const (
	trunkRear   = "rear"
	trunkFront  = "front"
	windowVent  = "vent"
	windowClose = "close"
)

var (
	trunkChoices  = []string{trunkRear, trunkFront}
	windowChoices = []string{windowVent, windowClose}
)

// Bounds of the optional window_control coordinates, in degrees, inclusive.
const (
	maxLatitude  = 90
	maxLongitude = 180
)

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
	// notRetried, when set and true for the body, reports a vehicle error without retry: the action
	// toggles a state (actuate_trunk rear), a retry after a lost reply would undo it (wimaha PR #162).
	notRetried func(args commandArgs) bool
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

	// UC1010: climate setpoints (ported from Lenart12, adapted).
	"set_temps": {
		validate: func(args commandArgs) error {
			_, _, err := args.cabinTemps()
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			driver, passenger, _ := args.cabinTemps() // validated by run
			if err := car.ChangeClimateTemp(ctx, driver, passenger); err != nil {
				return fmt.Errorf("failed to set temps to %.1f/%.1f: %w", driver, passenger, err)
			}
			return nil
		},
	},
	"set_preconditioning_max": {
		validate: func(args commandArgs) error {
			if _, err := args.boolArg("on"); err != nil {
				return err
			}
			_, err := args.optBoolArg("manual_override")
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			on, _ := args.boolArg("on")                             // validated by run
			manualOverride, _ := args.optBoolArg("manual_override") // validated by run
			if err := car.SetPreconditioningMax(ctx, on, manualOverride); err != nil {
				return fmt.Errorf("failed to set preconditioning max: %w", err)
			}
			return nil
		},
	},

	// UC1011: climate keeper, cabin overheat protection and bioweapon defense (ported from Lenart12, adapted).
	"set_climate_keeper_mode": {
		validate: func(args commandArgs) error {
			_, err := args.enumArg("climate_keeper_mode", len(climateKeeperModes))
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			mode, _ := args.enumArg("climate_keeper_mode", len(climateKeeperModes)) // validated by run
			if err := car.SetClimateKeeperMode(ctx, climateKeeperModes[mode], climateKeeperManualOverride); err != nil {
				return fmt.Errorf("failed to set climate keeper mode to %d: %w", mode, err)
			}
			return nil
		},
	},
	"set_cabin_overheat_protection": {
		validate: func(args commandArgs) error {
			if _, err := args.boolArg("on"); err != nil {
				return err
			}
			_, err := args.optBoolArg("fan_only")
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			on, _ := args.boolArg("on")               // validated by run
			fanOnly, _ := args.optBoolArg("fan_only") // validated by run
			if err := car.SetCabinOverheatProtection(ctx, on, fanOnly); err != nil {
				return fmt.Errorf("failed to set cabin overheat protection: %w", err)
			}
			return nil
		},
	},
	"set_cop_temp": {
		validate: func(args commandArgs) error {
			_, err := args.enumArg("cop_temp", len(copActivationLevels))
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			copTemp, _ := args.enumArg("cop_temp", len(copActivationLevels)) // validated by run
			if err := car.SetCabinOverheatProtectionTemperature(ctx, copActivationLevels[copTemp]); err != nil {
				return fmt.Errorf("failed to set cabin overheat protection temperature to cop_temp %d: %w", copTemp, err)
			}
			return nil
		},
	},
	"set_bioweapon_mode": {
		validate: func(args commandArgs) error {
			if _, err := args.boolArg("on"); err != nil {
				return err
			}
			_, err := args.optBoolArg("manual_override")
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			on, _ := args.boolArg("on")                             // validated by run
			manualOverride, _ := args.optBoolArg("manual_override") // validated by run
			if err := car.SetBioweaponDefenseMode(ctx, on, manualOverride); err != nil {
				return fmt.Errorf("failed to set bioweapon mode: %w", err)
			}
			return nil
		},
	},

	// UC1012: seat heaters and coolers, auto seat climate and steering wheel heater (ported from Lenart12, adapted).
	"remote_seat_heater_request": {
		validate: func(args commandArgs) error {
			if _, err := args.enumArg("heater", len(seatHeaterPositions)); err != nil {
				return err
			}
			_, err := args.enumArg("level", len(seatLevels))
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			heater, _ := args.enumArg("heater", len(seatHeaterPositions)) // validated by run
			level, _ := args.enumArg("level", len(seatLevels))            // validated by run
			levels := map[vehicle.SeatPosition]vehicle.Level{seatHeaterPositions[heater]: seatLevels[level]}
			if err := car.SetSeatHeater(ctx, levels); err != nil {
				return fmt.Errorf("failed to set seat heater %d to level %d: %w", heater, level, err)
			}
			return nil
		},
	},
	"remote_seat_cooler_request": {
		validate: func(args commandArgs) error {
			if _, err := args.frontSeatArg("seat_position"); err != nil {
				return err
			}
			_, err := args.enumArg("seat_cooler_level", len(seatLevels))
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			seat, _ := args.frontSeatArg("seat_position")                  // validated by run
			level, _ := args.enumArg("seat_cooler_level", len(seatLevels)) // validated by run
			if err := car.SetSeatCooler(ctx, seatLevels[level], frontSeats[seat]); err != nil {
				return fmt.Errorf("failed to set seat cooler at seat_position %d to level %d: %w", seat, level, err)
			}
			return nil
		},
	},
	"remote_auto_seat_climate_request": {
		validate: func(args commandArgs) error {
			if _, err := args.frontSeatArg("auto_seat_position"); err != nil {
				return err
			}
			_, err := args.boolArg("auto_climate_on")
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			seat, _ := args.frontSeatArg("auto_seat_position") // validated by run
			on, _ := args.boolArg("auto_climate_on")           // validated by run
			if err := car.AutoSeatAndClimate(ctx, []vehicle.SeatPosition{frontSeats[seat]}, on); err != nil {
				return fmt.Errorf("failed to set auto seat climate at auto_seat_position %d: %w", seat, err)
			}
			return nil
		},
	},
	"remote_steering_wheel_heater_request": {
		validate: func(args commandArgs) error {
			_, err := args.boolArg("on")
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			on, _ := args.boolArg("on") // validated by run
			if err := car.SetSteeringWheelHeater(ctx, on); err != nil {
				return fmt.Errorf("failed to set steering wheel heater: %w", err)
			}
			return nil
		},
	},

	// UC1013: trunk and window commands (ported from Lenart12, adapted; actuate_trunk follows wimaha PR #162).
	"actuate_trunk": {
		validate: func(args commandArgs) error {
			_, err := args.choiceArg("which_trunk", trunkChoices...)
			return err
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			which, _ := args.choiceArg("which_trunk", trunkChoices...) // validated by run
			switch which {
			case trunkFront:
				if err := car.OpenFrunk(ctx); err != nil {
					return fmt.Errorf("failed to open frunk: %w", err)
				}
			case trunkRear:
				// ActuateTrunk toggles: it opens a closed trunk and closes an open motorized one.
				if err := car.ActuateTrunk(ctx); err != nil {
					return fmt.Errorf("failed to actuate trunk (not retried, it is a toggle): %w", err)
				}
			default:
				return fmt.Errorf("unexpected which_trunk %q", which)
			}
			return nil
		},
		// Fail-safe: anything but the frunk is treated as a toggle and never retried.
		notRetried: func(args commandArgs) bool {
			which, _ := args.choiceArg("which_trunk", trunkChoices...)
			return which != trunkFront
		},
	},
	"window_control": {
		validate: func(args commandArgs) error {
			if _, err := args.choiceArg("command", windowChoices...); err != nil {
				return err
			}
			if err := args.optCoordinateArg("lat", maxLatitude); err != nil {
				return err
			}
			return args.optCoordinateArg("lon", maxLongitude)
		},
		execute: func(ctx context.Context, car vehicleCommander, args commandArgs) error {
			// lat and lon are validated but not sent: the SDK takes no position (BLE needs none).
			command, _ := args.choiceArg("command", windowChoices...) // validated by run
			switch command {
			case windowVent:
				if err := car.VentWindows(ctx); err != nil {
					return fmt.Errorf("failed to vent windows: %w", err)
				}
			case windowClose:
				if err := car.CloseWindows(ctx); err != nil {
					return fmt.Errorf("failed to close windows: %w", err)
				}
			default:
				return fmt.Errorf("unexpected window command %q", command)
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
// applies checkError. A body error is not retried; a vehicle error is retried, as in 2.3.0,
// unless the handler's notRetried says otherwise.
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
			return handler.notRetried == nil || !handler.notRetried(args), err
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

// intRangeArg reads a required Fleet integer in [lo, hi]: int32Arg tolerance and messages
// (JSON number or decimal string), plus a refusal of fractions on the raw JSON number, so that
// 2.5 is not read as 2 and -0.5 not as 0 (2.0 is accepted). Unlike int32Arg, which truncates
// fractions on the 14 commands of 2.3.0, a mode must never be guessed. NaN is not equal to its
// truncation and is refused as a fraction; infinities fall to int32Arg's out of range.
func (args commandArgs) intRangeArg(key string, lo, hi int) (int, error) {
	if f, ok := args[key].(float64); ok && f != math.Trunc(f) {
		return 0, invalidBodyf("%s must be an integer between %d and %d", key, lo, hi)
	}
	n, err := args.int32Arg(key)
	if err != nil {
		return 0, err
	}
	if int(n) < lo || int(n) > hi {
		return 0, invalidBodyf("%s must be an integer between %d and %d", key, lo, hi)
	}
	return int(n), nil
}

// enumArg reads a required Fleet enum index in [0, count), with the rules of intRangeArg.
func (args commandArgs) enumArg(key string, count int) (int, error) {
	return args.intRangeArg(key, 0, count-1)
}

// frontSeatArg reads a required front seat index of frontSeats (1 front left, 2 front right).
func (args commandArgs) frontSeatArg(key string) (int, error) {
	return args.intRangeArg(key, firstFrontSeat, len(frontSeats)-1)
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

// optBoolArg reads an optional boolean key: absent or null is false, otherwise as boolArg.
func (args commandArgs) optBoolArg(key string) (bool, error) {
	if args[key] == nil {
		return false, nil
	}
	return args.boolArg(key)
}

// optFloatArg reads an optional finite number: a JSON number or a decimal string
// (strconv.ParseFloat after TrimSpace, as wimaha PR #162; int32Arg does not trim). The decimal
// separator is a point. NaN and infinities are refused, whatever their spelling.
func (args commandArgs) optFloatArg(key string) (value float64, present bool, err error) {
	switch v := args[key].(type) {
	case nil:
		return 0, false, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, true, invalidBodyf("%s is not a valid number", key)
		}
		return v, true, nil
	case string:
		f, perr := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if errors.Is(perr, strconv.ErrRange) {
			return 0, true, invalidBodyf("%s is out of range", key)
		}
		if perr != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, true, invalidBodyf("%s is not a valid number", key)
		}
		return f, true, nil
	default:
		return 0, true, invalidBodyf("%s must be a number or a numeric string", key)
	}
}

// cabinTemps reads driver_temp (required) and passenger_temp (optional, defaults to the driver
// setpoint), both within [minCabinTempCelsius, maxCabinTempCelsius]. The body is not modified.
func (args commandArgs) cabinTemps() (driver, passenger float32, err error) {
	driverValue, present, err := args.optFloatArg("driver_temp")
	if err != nil {
		return 0, 0, err
	}
	if !present {
		return 0, 0, invalidBodyf("driver_temp missing")
	}
	if err := checkCabinTemp("driver_temp", driverValue); err != nil {
		return 0, 0, err
	}
	passengerValue, present, err := args.optFloatArg("passenger_temp")
	if err != nil {
		return 0, 0, err
	}
	if !present {
		passengerValue = driverValue
	}
	if err := checkCabinTemp("passenger_temp", passengerValue); err != nil {
		return 0, 0, err
	}
	return float32(driverValue), float32(passengerValue), nil
}

func checkCabinTemp(key string, v float64) error {
	if math.IsNaN(v) || v < minCabinTempCelsius || v > maxCabinTempCelsius {
		return invalidBodyf("%s must be between %d and %d degrees Celsius", key, minCabinTempCelsius, maxCabinTempCelsius)
	}
	return nil
}

// choiceArg reads a required string key, normalized by TrimSpace and ToLower (not EqualFold,
// so that the long s, U+017F, does not match "s"), and returns the canonical choice.
// The client's value is never echoed.
func (args commandArgs) choiceArg(key string, choices ...string) (string, error) {
	switch v := args[key].(type) {
	case nil:
		return "", invalidBodyf("%s missing", key)
	case string:
		value := strings.ToLower(strings.TrimSpace(v))
		if i := slices.Index(choices, value); i >= 0 {
			return choices[i], nil
		}
		quoted := make([]string, len(choices))
		for i, choice := range choices {
			quoted[i] = strconv.Quote(choice)
		}
		return "", invalidBodyf("%s must be %s", key, strings.Join(quoted, " or "))
	default:
		return "", invalidBodyf("%s must be a string", key)
	}
}

// optCoordinateArg reads an optional number (optFloatArg) that must lie in [-limit, limit].
func (args commandArgs) optCoordinateArg(key string, limit int) error {
	value, present, err := args.optFloatArg(key)
	if err != nil || !present {
		return err
	}
	if value < -float64(limit) || value > float64(limit) {
		return invalidBodyf("%s must be between -%d and %d degrees", key, limit, limit)
	}
	return nil
}
