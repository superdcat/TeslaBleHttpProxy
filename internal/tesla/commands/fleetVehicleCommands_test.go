package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/universalmessage"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
)

// commandsOf230 are the 14 vehicle commands of wimaha 2.3.0, migrated to the registry.
var commandsOf230 = []string{
	"auto_conditioning_start", "auto_conditioning_stop", "charge_port_door_open", "charge_port_door_close",
	"flash_lights", "wake_up", "set_charging_amps", "set_charge_limit", "charge_start", "charge_stop",
	"honk_horn", "door_lock", "door_unlock", "set_sentry_mode",
}

// fakeCar records the SDK calls of the registry; err is returned by every call.
type fakeCar struct {
	calls []string
	err   error
}

func (f *fakeCar) record(call string) error {
	f.calls = append(f.calls, call)
	return f.err
}

func (f *fakeCar) ClimateOn(context.Context) error       { return f.record("ClimateOn") }
func (f *fakeCar) ClimateOff(context.Context) error      { return f.record("ClimateOff") }
func (f *fakeCar) ChargePortOpen(context.Context) error  { return f.record("ChargePortOpen") }
func (f *fakeCar) ChargePortClose(context.Context) error { return f.record("ChargePortClose") }
func (f *fakeCar) FlashLights(context.Context) error     { return f.record("FlashLights") }
func (f *fakeCar) Wakeup(context.Context) error          { return f.record("Wakeup") }
func (f *fakeCar) HonkHorn(context.Context) error        { return f.record("HonkHorn") }
func (f *fakeCar) Lock(context.Context) error            { return f.record("Lock") }
func (f *fakeCar) Unlock(context.Context) error          { return f.record("Unlock") }
func (f *fakeCar) ChargeStart(context.Context) error     { return f.record("ChargeStart") }
func (f *fakeCar) ChargeStop(context.Context) error      { return f.record("ChargeStop") }
func (f *fakeCar) SetSentryMode(_ context.Context, state bool) error {
	return f.record(fmt.Sprintf("SetSentryMode(%t)", state))
}
func (f *fakeCar) SetChargingAmps(_ context.Context, amps int32) error {
	return f.record(fmt.Sprintf("SetChargingAmps(%d)", amps))
}
func (f *fakeCar) ChangeClimateTemp(_ context.Context, driver, passenger float32) error {
	return f.record(fmt.Sprintf("ChangeClimateTemp(%g,%g)", driver, passenger))
}
func (f *fakeCar) SetPreconditioningMax(_ context.Context, on, manualOverride bool) error {
	return f.record(fmt.Sprintf("SetPreconditioningMax(%t,%t)", on, manualOverride))
}
func (f *fakeCar) SetClimateKeeperMode(_ context.Context, mode vehicle.ClimateKeeperMode, override bool) error {
	return f.record(fmt.Sprintf("SetClimateKeeperMode(%s,%t)", mode, override))
}
func (f *fakeCar) SetCabinOverheatProtection(_ context.Context, on, fanOnly bool) error {
	return f.record(fmt.Sprintf("SetCabinOverheatProtection(%t,%t)", on, fanOnly))
}

// Same cast as the SDK (climate.go): the level becomes the protobuf activation temperature.
func (f *fakeCar) SetCabinOverheatProtectionTemperature(_ context.Context, level vehicle.Level) error {
	return f.record(fmt.Sprintf("SetCabinOverheatProtectionTemperature(%s)", carserver.ClimateState_CopActivationTemp(level)))
}
func (f *fakeCar) SetBioweaponDefenseMode(_ context.Context, on, manualOverride bool) error {
	return f.record(fmt.Sprintf("SetBioweaponDefenseMode(%t,%t)", on, manualOverride))
}

// SetSeatHeater records the entries sorted by seat, named by hand per SDK constant (sdkSeatName),
// so the expectation does not depend on the Fleet integer.
func (f *fakeCar) SetSeatHeater(_ context.Context, levels map[vehicle.SeatPosition]vehicle.Level) error {
	seats := slices.Sorted(maps.Keys(levels))
	parts := make([]string, 0, len(seats))
	for _, seat := range seats {
		parts = append(parts, sdkSeatName(seat)+"="+sdkLevelName(levels[seat]))
	}
	return f.record("SetSeatHeater(" + strings.Join(parts, ",") + ")")
}

// SetSeatCooler uses the same cast as the SDK (climate.go): the level is shifted by one.
func (f *fakeCar) SetSeatCooler(_ context.Context, level vehicle.Level, seat vehicle.SeatPosition) error {
	return f.record(fmt.Sprintf("SetSeatCooler(%s,%s)", sdkSeatName(seat),
		carserver.HvacSeatCoolerActions_HvacSeatCoolerLevel_E(level+1)))
}
func (f *fakeCar) AutoSeatAndClimate(_ context.Context, positions []vehicle.SeatPosition, on bool) error {
	names := make([]string, 0, len(positions))
	for _, seat := range positions {
		names = append(names, sdkSeatName(seat))
	}
	return f.record(fmt.Sprintf("AutoSeatAndClimate([%s],%t)", strings.Join(names, ","), on))
}
func (f *fakeCar) SetSteeringWheelHeater(_ context.Context, on bool) error {
	return f.record(fmt.Sprintf("SetSteeringWheelHeater(%t)", on))
}
func (f *fakeCar) OpenFrunk(context.Context) error    { return f.record("OpenFrunk") }
func (f *fakeCar) ActuateTrunk(context.Context) error { return f.record("ActuateTrunk") }
func (f *fakeCar) VentWindows(context.Context) error  { return f.record("VentWindows") }
func (f *fakeCar) CloseWindows(context.Context) error { return f.record("CloseWindows") }

// sdkSeatName names an SDK seat constant by hand (the SDK has no String()).
func sdkSeatName(seat vehicle.SeatPosition) string {
	switch seat {
	case vehicle.SeatUnknown:
		return "SeatUnknown"
	case vehicle.SeatFrontLeft:
		return "SeatFrontLeft"
	case vehicle.SeatFrontRight:
		return "SeatFrontRight"
	case vehicle.SeatSecondRowLeft:
		return "SeatSecondRowLeft"
	case vehicle.SeatSecondRowLeftBack:
		return "SeatSecondRowLeftBack"
	case vehicle.SeatSecondRowCenter:
		return "SeatSecondRowCenter"
	case vehicle.SeatSecondRowRight:
		return "SeatSecondRowRight"
	case vehicle.SeatSecondRowRightBack:
		return "SeatSecondRowRightBack"
	case vehicle.SeatThirdRowLeft:
		return "SeatThirdRowLeft"
	case vehicle.SeatThirdRowRight:
		return "SeatThirdRowRight"
	}
	return fmt.Sprintf("seat(%d)", int(seat))
}

// sdkLevelName names an SDK level constant by hand.
func sdkLevelName(level vehicle.Level) string {
	switch level {
	case vehicle.LevelOff:
		return "LevelOff"
	case vehicle.LevelLow:
		return "LevelLow"
	case vehicle.LevelMed:
		return "LevelMed"
	case vehicle.LevelHigh:
		return "LevelHigh"
	}
	return fmt.Sprintf("level(%d)", int(level))
}
func (f *fakeCar) ChangeChargeLimit(_ context.Context, percent int32) error {
	return f.record(fmt.Sprintf("ChangeChargeLimit(%d)", percent))
}

// decodeBody decodes raw as the command handler does (nil map when absent or unreadable).
func decodeBody(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatalf("test body %s is not a JSON object: %v", raw, err)
		}
	}
	return body
}

// The 14 commands of 2.3.0 stay in the registry (later UCs only add commands).
func TestRegistryKeeps230Commands(t *testing.T) {
	for _, name := range commandsOf230 {
		if _, ok := fleetVehicleCommands[name]; !ok {
			t.Errorf("command %q of 2.3.0 is missing from the registry", name)
		}
	}
}

// AC4: no command is announced without being wired.
func TestRegistryCommandsHaveExecutor(t *testing.T) {
	for name, handler := range fleetVehicleCommands {
		if handler.execute == nil {
			t.Errorf("command %q is in the registry without execute", name)
		}
	}
}

func TestSupportedCommandsKeep230Route(t *testing.T) {
	for _, name := range append(slices.Clone(commandsOf230), legacyRouteCommands...) {
		if !IsSupportedCommand(name) {
			t.Errorf("IsSupportedCommand(%q) = false, want true (accepted by 2.3.0)", name)
		}
	}
	for _, name := range []string{"", "x", "add-key-request", "body-controller-state", "Flash_Lights"} {
		if IsSupportedCommand(name) {
			t.Errorf("IsSupportedCommand(%q) = true, want false", name)
		}
	}
}

// AC1, AC2, AC3, AC5: valid and invalid bodies of the 14 commands, checked before queuing
// (ValidateCommandBody) and again at execution (run), without vehicle or BLE adapter.
func TestCommandBodies(t *testing.T) {
	const missingAmps = "invalid request body: charging_amps missing"
	tests := []struct {
		command string
		body    string // JSON object; "" = no body
		call    string // SDK call expected for a valid body
		reason  string // error expected for an invalid body
	}{
		// Commands without body: any body is ignored, as in 2.3.0.
		{"auto_conditioning_start", "", "ClimateOn", ""},
		{"auto_conditioning_start", `{"foo":1}`, "ClimateOn", ""},
		{"auto_conditioning_stop", "", "ClimateOff", ""},
		{"auto_conditioning_stop", `{}`, "ClimateOff", ""},
		{"charge_port_door_open", "", "ChargePortOpen", ""},
		{"charge_port_door_open", `{"on":"maybe"}`, "ChargePortOpen", ""},
		{"charge_port_door_close", "", "ChargePortClose", ""},
		{"charge_port_door_close", `{}`, "ChargePortClose", ""},
		{"flash_lights", "", "FlashLights", ""},
		{"flash_lights", `{"x":[1,2]}`, "FlashLights", ""},
		{"wake_up", "", "Wakeup", ""},
		{"wake_up", `{}`, "Wakeup", ""},
		{"honk_horn", "", "HonkHorn", ""},
		{"honk_horn", `{}`, "HonkHorn", ""},
		{"door_lock", "", "Lock", ""},
		{"door_lock", `{}`, "Lock", ""},
		{"door_unlock", "", "Unlock", ""},
		{"door_unlock", `{}`, "Unlock", ""},
		{"charge_start", "", "ChargeStart", ""},
		{"charge_start", `{}`, "ChargeStart", ""},
		{"charge_stop", "", "ChargeStop", ""},
		{"charge_stop", `{}`, "ChargeStop", ""},

		// set_charging_amps: number, numeric string, decimal truncated, no bound (AC2, AC5).
		{"set_charging_amps", `{"charging_amps":16}`, "SetChargingAmps(16)", ""},
		{"set_charging_amps", `{"charging_amps":"16"}`, "SetChargingAmps(16)", ""},
		{"set_charging_amps", `{"charging_amps":16.0}`, "SetChargingAmps(16)", ""},
		{"set_charging_amps", `{"charging_amps":-1.5}`, "SetChargingAmps(-1)", ""},
		{"set_charging_amps", `{"charging_amps":16.5}`, "SetChargingAmps(16)", ""},
		{"set_charging_amps", `{"charging_amps":"+16"}`, "SetChargingAmps(16)", ""},
		{"set_charging_amps", `{"charging_amps":72}`, "SetChargingAmps(72)", ""},
		{"set_charging_amps", `{"charging_amps":0}`, "SetChargingAmps(0)", ""},
		{"set_charging_amps", `{"charging_amps":-1}`, "SetChargingAmps(-1)", ""},
		{"set_charging_amps", `{"charging_amps":16,"extra":true}`, "SetChargingAmps(16)", ""},
		{"set_charging_amps", "", "", missingAmps},
		{"set_charging_amps", `{}`, "", missingAmps},
		{"set_charging_amps", `{"charging_amps":null}`, "", missingAmps},
		{"set_charging_amps", `{"chargingAmps":16}`, "", missingAmps},
		{"set_charging_amps", `{"charging_amps":"abc"}`, "", "invalid request body: charging_amps is not a valid integer"},
		{"set_charging_amps", `{"charging_amps":"16.5"}`, "", "invalid request body: charging_amps is not a valid integer"},
		{"set_charging_amps", `{"charging_amps":""}`, "", "invalid request body: charging_amps is not a valid integer"},
		{"set_charging_amps", `{"charging_amps":true}`, "", "invalid request body: charging_amps must be a number or a numeric string"},
		{"set_charging_amps", `{"charging_amps":[16]}`, "", "invalid request body: charging_amps must be a number or a numeric string"},
		{"set_charging_amps", `{"charging_amps":3e9}`, "", "invalid request body: charging_amps is out of range"},
		{"set_charging_amps", `{"charging_amps":"3000000000"}`, "", "invalid request body: charging_amps is out of range"},

		// set_charge_limit: same tolerance, no 50-100 bound (AC5).
		{"set_charge_limit", `{"percent":80}`, "ChangeChargeLimit(80)", ""},
		{"set_charge_limit", `{"percent":"80"}`, "ChangeChargeLimit(80)", ""},
		{"set_charge_limit", `{"percent":80.9}`, "ChangeChargeLimit(80)", ""},
		{"set_charge_limit", `{"percent":10}`, "ChangeChargeLimit(10)", ""},
		{"set_charge_limit", `{"percent":100}`, "ChangeChargeLimit(100)", ""},
		{"set_charge_limit", "", "", "invalid request body: percent missing"},
		{"set_charge_limit", `{}`, "", "invalid request body: percent missing"},
		{"set_charge_limit", `{"percent":"eighty"}`, "", "invalid request body: percent is not a valid integer"},
		{"set_charge_limit", `{"percent":false}`, "", "invalid request body: percent must be a number or a numeric string"},

		// set_sentry_mode: boolean or strconv.ParseBool string (AC2).
		{"set_sentry_mode", `{"on":true}`, "SetSentryMode(true)", ""},
		{"set_sentry_mode", `{"on":false}`, "SetSentryMode(false)", ""},
		{"set_sentry_mode", `{"on":"true"}`, "SetSentryMode(true)", ""},
		{"set_sentry_mode", `{"on":"false"}`, "SetSentryMode(false)", ""},
		{"set_sentry_mode", `{"on":"1"}`, "SetSentryMode(true)", ""},
		{"set_sentry_mode", "", "", "invalid request body: on missing"},
		{"set_sentry_mode", `{}`, "", "invalid request body: on missing"},
		{"set_sentry_mode", `{"on":null}`, "", "invalid request body: on missing"},
		{"set_sentry_mode", `{"on":"yes"}`, "", "invalid request body: on is not a valid boolean"},
		{"set_sentry_mode", `{"on":1}`, "", `invalid request body: on must be a boolean or "true"/"false"`},

		// set_temps (UC1010): 15-28 degrees Celsius inclusive, passenger defaults to driver.
		{"set_temps", `{"driver_temp":21.5,"passenger_temp":20}`, "ChangeClimateTemp(21.5,20)", ""},
		{"set_temps", `{"driver_temp":21}`, "ChangeClimateTemp(21,21)", ""},
		{"set_temps", `{"driver_temp":21,"passenger_temp":null}`, "ChangeClimateTemp(21,21)", ""},
		{"set_temps", `{"driver_temp":15,"passenger_temp":28}`, "ChangeClimateTemp(15,28)", ""},
		{"set_temps", `{"driver_temp":28,"passenger_temp":15}`, "ChangeClimateTemp(28,15)", ""},
		{"set_temps", `{"driver_temp":"22","passenger_temp":" 19.5 "}`, "ChangeClimateTemp(22,19.5)", ""},
		{"set_temps", `{"driver_temp":"15.0"}`, "ChangeClimateTemp(15,15)", ""},
		{"set_temps", `{"driver_temp":21.1}`, "ChangeClimateTemp(21.1,21.1)", ""},
		{"set_temps", `{"driver_temp":21,"extra":true}`, "ChangeClimateTemp(21,21)", ""},
		{"set_temps", ``, "", "invalid request body: driver_temp missing"},
		{"set_temps", `{}`, "", "invalid request body: driver_temp missing"},
		{"set_temps", `{"driver_temp":null}`, "", "invalid request body: driver_temp missing"},
		{"set_temps", `{"passenger_temp":20}`, "", "invalid request body: driver_temp missing"},
		{"set_temps", `{"driver_temp":14.9}`, "", "invalid request body: driver_temp must be between 15 and 28 degrees Celsius"},
		{"set_temps", `{"driver_temp":28.1}`, "", "invalid request body: driver_temp must be between 15 and 28 degrees Celsius"},
		{"set_temps", `{"driver_temp":"14.9"}`, "", "invalid request body: driver_temp must be between 15 and 28 degrees Celsius"},
		{"set_temps", `{"driver_temp":70}`, "", "invalid request body: driver_temp must be between 15 and 28 degrees Celsius"},
		{"set_temps", `{"driver_temp":21,"passenger_temp":28.1}`, "", "invalid request body: passenger_temp must be between 15 and 28 degrees Celsius"},
		{"set_temps", `{"driver_temp":21,"passenger_temp":14.9}`, "", "invalid request body: passenger_temp must be between 15 and 28 degrees Celsius"},
		{"set_temps", `{"driver_temp":"warm"}`, "", "invalid request body: driver_temp is not a valid number"},
		{"set_temps", `{"driver_temp":""}`, "", "invalid request body: driver_temp is not a valid number"},
		{"set_temps", `{"driver_temp":"  "}`, "", "invalid request body: driver_temp is not a valid number"},
		{"set_temps", `{"driver_temp":"NaN"}`, "", "invalid request body: driver_temp is not a valid number"},
		{"set_temps", `{"driver_temp":"Inf"}`, "", "invalid request body: driver_temp is not a valid number"},
		{"set_temps", `{"driver_temp":"-Inf"}`, "", "invalid request body: driver_temp is not a valid number"},
		{"set_temps", `{"driver_temp":"Infinity"}`, "", "invalid request body: driver_temp is not a valid number"},
		{"set_temps", `{"driver_temp":"21,5"}`, "", "invalid request body: driver_temp is not a valid number"},
		{"set_temps", `{"driver_temp":"1e999"}`, "", "invalid request body: driver_temp is out of range"},
		{"set_temps", `{"driver_temp":true}`, "", "invalid request body: driver_temp must be a number or a numeric string"},
		{"set_temps", `{"driver_temp":[21]}`, "", "invalid request body: driver_temp must be a number or a numeric string"},
		{"set_temps", `{"driver_temp":21,"passenger_temp":"x"}`, "", "invalid request body: passenger_temp is not a valid number"},
		{"set_temps", `{"driver_temp":21,"passenger_temp":false}`, "", "invalid request body: passenger_temp must be a number or a numeric string"},
		{"set_temps", `{"driver_temp":21,"passenger_temp":""}`, "", "invalid request body: passenger_temp is not a valid number"},

		// set_preconditioning_max (UC1010): on required, manual_override optional (false).
		{"set_preconditioning_max", `{"on":true}`, "SetPreconditioningMax(true,false)", ""},
		{"set_preconditioning_max", `{"on":false}`, "SetPreconditioningMax(false,false)", ""},
		{"set_preconditioning_max", `{"on":true,"manual_override":true}`, "SetPreconditioningMax(true,true)", ""},
		{"set_preconditioning_max", `{"on":"true","manual_override":"false"}`, "SetPreconditioningMax(true,false)", ""},
		{"set_preconditioning_max", `{"on":true,"manual_override":null}`, "SetPreconditioningMax(true,false)", ""},
		{"set_preconditioning_max", ``, "", "invalid request body: on missing"},
		{"set_preconditioning_max", `{}`, "", "invalid request body: on missing"},
		{"set_preconditioning_max", `{"manual_override":true}`, "", "invalid request body: on missing"},
		{"set_preconditioning_max", `{"on":"yes"}`, "", "invalid request body: on is not a valid boolean"},
		{"set_preconditioning_max", `{"on":1}`, "", `invalid request body: on must be a boolean or "true"/"false"`},
		{"set_preconditioning_max", `{"on":true,"manual_override":"yes"}`, "", "invalid request body: manual_override is not a valid boolean"},
		{"set_preconditioning_max", `{"on":true,"manual_override":1}`, "", `invalid request body: manual_override must be a boolean or "true"/"false"`},

		// set_climate_keeper_mode (UC1011): 0 off, 1 on, 2 dog, 3 camp; manual_override always true.
		{"set_climate_keeper_mode", `{"climate_keeper_mode":0}`, "SetClimateKeeperMode(ClimateKeeperAction_Off,true)", ""},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":1}`, "SetClimateKeeperMode(ClimateKeeperAction_On,true)", ""},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":2}`, "SetClimateKeeperMode(ClimateKeeperAction_Dog,true)", ""},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":3}`, "SetClimateKeeperMode(ClimateKeeperAction_Camp,true)", ""},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":"2"}`, "SetClimateKeeperMode(ClimateKeeperAction_Dog,true)", ""},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":2.0}`, "SetClimateKeeperMode(ClimateKeeperAction_Dog,true)", ""},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":"+3"}`, "SetClimateKeeperMode(ClimateKeeperAction_Camp,true)", ""},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":2,"manual_override":false}`, "SetClimateKeeperMode(ClimateKeeperAction_Dog,true)", ""},
		{"set_climate_keeper_mode", ``, "", "invalid request body: climate_keeper_mode missing"},
		{"set_climate_keeper_mode", `{}`, "", "invalid request body: climate_keeper_mode missing"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":null}`, "", "invalid request body: climate_keeper_mode missing"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":4}`, "", "invalid request body: climate_keeper_mode must be an integer between 0 and 3"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":-1}`, "", "invalid request body: climate_keeper_mode must be an integer between 0 and 3"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":"4"}`, "", "invalid request body: climate_keeper_mode must be an integer between 0 and 3"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":2.5}`, "", "invalid request body: climate_keeper_mode must be an integer between 0 and 3"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":-0.5}`, "", "invalid request body: climate_keeper_mode must be an integer between 0 and 3"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":"dog"}`, "", "invalid request body: climate_keeper_mode is not a valid integer"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":""}`, "", "invalid request body: climate_keeper_mode is not a valid integer"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":" 2"}`, "", "invalid request body: climate_keeper_mode is not a valid integer"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":"2.0"}`, "", "invalid request body: climate_keeper_mode is not a valid integer"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":true}`, "", "invalid request body: climate_keeper_mode must be a number or a numeric string"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":[2]}`, "", "invalid request body: climate_keeper_mode must be a number or a numeric string"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":3e9}`, "", "invalid request body: climate_keeper_mode is out of range"},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":"3000000000"}`, "", "invalid request body: climate_keeper_mode is out of range"},

		// set_cabin_overheat_protection (UC1011): on required, fan_only optional (false).
		{"set_cabin_overheat_protection", `{"on":true}`, "SetCabinOverheatProtection(true,false)", ""},
		{"set_cabin_overheat_protection", `{"on":false}`, "SetCabinOverheatProtection(false,false)", ""},
		{"set_cabin_overheat_protection", `{"on":true,"fan_only":true}`, "SetCabinOverheatProtection(true,true)", ""},
		{"set_cabin_overheat_protection", `{"on":"true","fan_only":"false"}`, "SetCabinOverheatProtection(true,false)", ""},
		{"set_cabin_overheat_protection", `{"on":true,"fan_only":null}`, "SetCabinOverheatProtection(true,false)", ""},
		{"set_cabin_overheat_protection", `{"on":false,"fan_only":true}`, "SetCabinOverheatProtection(false,true)", ""},
		{"set_cabin_overheat_protection", ``, "", "invalid request body: on missing"},
		{"set_cabin_overheat_protection", `{}`, "", "invalid request body: on missing"},
		{"set_cabin_overheat_protection", `{"fan_only":true}`, "", "invalid request body: on missing"},
		{"set_cabin_overheat_protection", `{"on":"yes"}`, "", "invalid request body: on is not a valid boolean"},
		{"set_cabin_overheat_protection", `{"on":true,"fan_only":"yes"}`, "", "invalid request body: fan_only is not a valid boolean"},
		{"set_cabin_overheat_protection", `{"on":1}`, "", `invalid request body: on must be a boolean or "true"/"false"`},
		{"set_cabin_overheat_protection", `{"on":true,"fan_only":1}`, "", `invalid request body: fan_only must be a boolean or "true"/"false"`},

		// set_cop_temp (UC1011): 0 low (30 C), 1 medium (35 C), 2 high (40 C).
		{"set_cop_temp", `{"cop_temp":0}`, "SetCabinOverheatProtectionTemperature(CopActivationTempLow)", ""},
		{"set_cop_temp", `{"cop_temp":1}`, "SetCabinOverheatProtectionTemperature(CopActivationTempMedium)", ""},
		{"set_cop_temp", `{"cop_temp":2}`, "SetCabinOverheatProtectionTemperature(CopActivationTempHigh)", ""},
		{"set_cop_temp", `{"cop_temp":"0"}`, "SetCabinOverheatProtectionTemperature(CopActivationTempLow)", ""},
		{"set_cop_temp", `{"cop_temp":2.0}`, "SetCabinOverheatProtectionTemperature(CopActivationTempHigh)", ""},
		{"set_cop_temp", ``, "", "invalid request body: cop_temp missing"},
		{"set_cop_temp", `{}`, "", "invalid request body: cop_temp missing"},
		{"set_cop_temp", `{"cop_temp":null}`, "", "invalid request body: cop_temp missing"},
		{"set_cop_temp", `{"cop_temp":3}`, "", "invalid request body: cop_temp must be an integer between 0 and 2"},
		{"set_cop_temp", `{"cop_temp":-1}`, "", "invalid request body: cop_temp must be an integer between 0 and 2"},
		{"set_cop_temp", `{"cop_temp":"3"}`, "", "invalid request body: cop_temp must be an integer between 0 and 2"},
		{"set_cop_temp", `{"cop_temp":1.5}`, "", "invalid request body: cop_temp must be an integer between 0 and 2"},
		{"set_cop_temp", `{"cop_temp":"high"}`, "", "invalid request body: cop_temp is not a valid integer"},
		{"set_cop_temp", `{"cop_temp":true}`, "", "invalid request body: cop_temp must be a number or a numeric string"},
		{"set_cop_temp", `{"cop_temp":3e9}`, "", "invalid request body: cop_temp is out of range"},

		// set_bioweapon_mode (UC1011): on required, manual_override optional (false).
		{"set_bioweapon_mode", `{"on":true}`, "SetBioweaponDefenseMode(true,false)", ""},
		{"set_bioweapon_mode", `{"on":false}`, "SetBioweaponDefenseMode(false,false)", ""},
		{"set_bioweapon_mode", `{"on":true,"manual_override":true}`, "SetBioweaponDefenseMode(true,true)", ""},
		{"set_bioweapon_mode", `{"on":"true","manual_override":"false"}`, "SetBioweaponDefenseMode(true,false)", ""},
		{"set_bioweapon_mode", `{"on":true,"manual_override":null}`, "SetBioweaponDefenseMode(true,false)", ""},
		{"set_bioweapon_mode", `{"on":false,"manual_override":true}`, "SetBioweaponDefenseMode(false,true)", ""},
		{"set_bioweapon_mode", ``, "", "invalid request body: on missing"},
		{"set_bioweapon_mode", `{}`, "", "invalid request body: on missing"},
		{"set_bioweapon_mode", `{"manual_override":true}`, "", "invalid request body: on missing"},
		{"set_bioweapon_mode", `{"on":"yes"}`, "", "invalid request body: on is not a valid boolean"},
		{"set_bioweapon_mode", `{"on":true,"manual_override":"yes"}`, "", "invalid request body: manual_override is not a valid boolean"},
		{"set_bioweapon_mode", `{"on":1}`, "", `invalid request body: on must be a boolean or "true"/"false"`},
		{"set_bioweapon_mode", `{"on":true,"manual_override":1}`, "", `invalid request body: manual_override must be a boolean or "true"/"false"`},

		// remote_seat_heater_request (UC1012): heater 0-8 (Fleet numbering), level 0-3.
		{"remote_seat_heater_request", `{"heater":0,"level":0}`, "SetSeatHeater(SeatFrontLeft=LevelOff)", ""},
		{"remote_seat_heater_request", `{"heater":1,"level":1}`, "SetSeatHeater(SeatFrontRight=LevelLow)", ""},
		{"remote_seat_heater_request", `{"heater":2,"level":2}`, "SetSeatHeater(SeatSecondRowLeft=LevelMed)", ""},
		{"remote_seat_heater_request", `{"heater":3,"level":3}`, "SetSeatHeater(SeatSecondRowLeftBack=LevelHigh)", ""},
		{"remote_seat_heater_request", `{"heater":4,"level":0}`, "SetSeatHeater(SeatSecondRowCenter=LevelOff)", ""},
		{"remote_seat_heater_request", `{"heater":5,"level":1}`, "SetSeatHeater(SeatSecondRowRight=LevelLow)", ""},
		{"remote_seat_heater_request", `{"heater":6,"level":2}`, "SetSeatHeater(SeatSecondRowRightBack=LevelMed)", ""},
		{"remote_seat_heater_request", `{"heater":7,"level":3}`, "SetSeatHeater(SeatThirdRowLeft=LevelHigh)", ""},
		{"remote_seat_heater_request", `{"heater":8,"level":0}`, "SetSeatHeater(SeatThirdRowRight=LevelOff)", ""},
		{"remote_seat_heater_request", `{"heater":6,"level":0}`, "SetSeatHeater(SeatSecondRowRightBack=LevelOff)", ""},
		{"remote_seat_heater_request", `{"heater":2,"level":3}`, "SetSeatHeater(SeatSecondRowLeft=LevelHigh)", ""},
		{"remote_seat_heater_request", `{"heater":"1","level":"2"}`, "SetSeatHeater(SeatFrontRight=LevelMed)", ""},
		{"remote_seat_heater_request", `{"heater":8.0,"level":3.0}`, "SetSeatHeater(SeatThirdRowRight=LevelHigh)", ""},
		{"remote_seat_heater_request", ``, "", "invalid request body: heater missing"},
		{"remote_seat_heater_request", `{}`, "", "invalid request body: heater missing"},
		{"remote_seat_heater_request", `{"heater":null,"level":1}`, "", "invalid request body: heater missing"},
		{"remote_seat_heater_request", `{"heater":0}`, "", "invalid request body: level missing"},
		{"remote_seat_heater_request", `{"level":1}`, "", "invalid request body: heater missing"},
		{"remote_seat_heater_request", `{"seat_position":0,"level":1}`, "", "invalid request body: heater missing"},
		{"remote_seat_heater_request", `{"heater":9,"level":1}`, "", "invalid request body: heater must be an integer between 0 and 8"},
		{"remote_seat_heater_request", `{"heater":-1,"level":1}`, "", "invalid request body: heater must be an integer between 0 and 8"},
		{"remote_seat_heater_request", `{"heater":0.5,"level":1}`, "", "invalid request body: heater must be an integer between 0 and 8"},
		{"remote_seat_heater_request", `{"heater":0,"level":4}`, "", "invalid request body: level must be an integer between 0 and 3"},
		{"remote_seat_heater_request", `{"heater":0,"level":-1}`, "", "invalid request body: level must be an integer between 0 and 3"},
		{"remote_seat_heater_request", `{"heater":0,"level":1.5}`, "", "invalid request body: level must be an integer between 0 and 3"},
		{"remote_seat_heater_request", `{"heater":9,"level":4}`, "", "invalid request body: heater must be an integer between 0 and 8"},
		{"remote_seat_heater_request", `{"heater":"9","level":1}`, "", "invalid request body: heater must be an integer between 0 and 8"},
		{"remote_seat_heater_request", `{"heater":"left","level":1}`, "", "invalid request body: heater is not a valid integer"},
		{"remote_seat_heater_request", `{"heater":"","level":1}`, "", "invalid request body: heater is not a valid integer"},
		{"remote_seat_heater_request", `{"heater":0,"level":" 1"}`, "", "invalid request body: level is not a valid integer"},
		{"remote_seat_heater_request", `{"heater":true,"level":1}`, "", "invalid request body: heater must be a number or a numeric string"},
		{"remote_seat_heater_request", `{"heater":0,"level":[1]}`, "", "invalid request body: level must be a number or a numeric string"},
		{"remote_seat_heater_request", `{"heater":3e9,"level":1}`, "", "invalid request body: heater is out of range"},
		{"remote_seat_heater_request", `{"heater":0,"level":"3000000000"}`, "", "invalid request body: level is out of range"},

		// remote_seat_cooler_request (UC1012): seat_position 1-2 (front left/right), seat_cooler_level 0-3 (0 = off).
		{"remote_seat_cooler_request", `{"seat_position":1,"seat_cooler_level":0}`, "SetSeatCooler(SeatFrontLeft,HvacSeatCoolerLevel_Off)", ""},
		{"remote_seat_cooler_request", `{"seat_position":2,"seat_cooler_level":3}`, "SetSeatCooler(SeatFrontRight,HvacSeatCoolerLevel_High)", ""},
		{"remote_seat_cooler_request", `{"seat_position":1,"seat_cooler_level":1}`, "SetSeatCooler(SeatFrontLeft,HvacSeatCoolerLevel_Low)", ""},
		{"remote_seat_cooler_request", `{"seat_position":"2","seat_cooler_level":"2"}`, "SetSeatCooler(SeatFrontRight,HvacSeatCoolerLevel_Med)", ""},
		{"remote_seat_cooler_request", `{"seat_position":2.0,"seat_cooler_level":0.0}`, "SetSeatCooler(SeatFrontRight,HvacSeatCoolerLevel_Off)", ""},
		{"remote_seat_cooler_request", ``, "", "invalid request body: seat_position missing"},
		{"remote_seat_cooler_request", `{}`, "", "invalid request body: seat_position missing"},
		{"remote_seat_cooler_request", `{"seat_position":1}`, "", "invalid request body: seat_cooler_level missing"},
		{"remote_seat_cooler_request", `{"seat_cooler_level":1}`, "", "invalid request body: seat_position missing"},
		{"remote_seat_cooler_request", `{"heater":1,"level":1}`, "", "invalid request body: seat_position missing"},
		{"remote_seat_cooler_request", `{"seat_position":0,"seat_cooler_level":1}`, "", "invalid request body: seat_position must be an integer between 1 and 2"},
		{"remote_seat_cooler_request", `{"seat_position":3,"seat_cooler_level":1}`, "", "invalid request body: seat_position must be an integer between 1 and 2"},
		{"remote_seat_cooler_request", `{"seat_position":1.5,"seat_cooler_level":1}`, "", "invalid request body: seat_position must be an integer between 1 and 2"},
		{"remote_seat_cooler_request", `{"seat_position":1,"seat_cooler_level":4}`, "", "invalid request body: seat_cooler_level must be an integer between 0 and 3"},
		{"remote_seat_cooler_request", `{"seat_position":1,"seat_cooler_level":-1}`, "", "invalid request body: seat_cooler_level must be an integer between 0 and 3"},
		{"remote_seat_cooler_request", `{"seat_position":"left","seat_cooler_level":1}`, "", "invalid request body: seat_position is not a valid integer"},
		{"remote_seat_cooler_request", `{"seat_position":true,"seat_cooler_level":1}`, "", "invalid request body: seat_position must be a number or a numeric string"},
		{"remote_seat_cooler_request", `{"seat_position":3e9,"seat_cooler_level":1}`, "", "invalid request body: seat_position is out of range"},

		// remote_auto_seat_climate_request (UC1012): auto_seat_position 1-2, auto_climate_on.
		{"remote_auto_seat_climate_request", `{"auto_seat_position":1,"auto_climate_on":true}`, "AutoSeatAndClimate([SeatFrontLeft],true)", ""},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":2,"auto_climate_on":false}`, "AutoSeatAndClimate([SeatFrontRight],false)", ""},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":"1","auto_climate_on":"true"}`, "AutoSeatAndClimate([SeatFrontLeft],true)", ""},
		{"remote_auto_seat_climate_request", ``, "", "invalid request body: auto_seat_position missing"},
		{"remote_auto_seat_climate_request", `{}`, "", "invalid request body: auto_seat_position missing"},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":1}`, "", "invalid request body: auto_climate_on missing"},
		{"remote_auto_seat_climate_request", `{"auto_climate_on":true}`, "", "invalid request body: auto_seat_position missing"},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":0,"auto_climate_on":true}`, "", "invalid request body: auto_seat_position must be an integer between 1 and 2"},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":3,"auto_climate_on":true}`, "", "invalid request body: auto_seat_position must be an integer between 1 and 2"},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":1.5,"auto_climate_on":true}`, "", "invalid request body: auto_seat_position must be an integer between 1 and 2"},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":"x","auto_climate_on":true}`, "", "invalid request body: auto_seat_position is not a valid integer"},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":1,"auto_climate_on":"yes"}`, "", "invalid request body: auto_climate_on is not a valid boolean"},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":1,"auto_climate_on":1}`, "", `invalid request body: auto_climate_on must be a boolean or "true"/"false"`},

		// remote_steering_wheel_heater_request (UC1012): on required.
		{"remote_steering_wheel_heater_request", `{"on":true}`, "SetSteeringWheelHeater(true)", ""},
		{"remote_steering_wheel_heater_request", `{"on":false}`, "SetSteeringWheelHeater(false)", ""},
		{"remote_steering_wheel_heater_request", `{"on":"true"}`, "SetSteeringWheelHeater(true)", ""},
		{"remote_steering_wheel_heater_request", `{"on":"0"}`, "SetSteeringWheelHeater(false)", ""},
		{"remote_steering_wheel_heater_request", ``, "", "invalid request body: on missing"},
		{"remote_steering_wheel_heater_request", `{}`, "", "invalid request body: on missing"},
		{"remote_steering_wheel_heater_request", `{"on":"yes"}`, "", "invalid request body: on is not a valid boolean"},
		{"remote_steering_wheel_heater_request", `{"on":1}`, "", `invalid request body: on must be a boolean or "true"/"false"`},

		// actuate_trunk (UC1013): which_trunk required, "rear" or "front", case and spaces tolerated.
		{"actuate_trunk", `{"which_trunk":"rear"}`, "ActuateTrunk", ""},
		{"actuate_trunk", `{"which_trunk":"front"}`, "OpenFrunk", ""},
		{"actuate_trunk", `{"which_trunk":" REAR "}`, "ActuateTrunk", ""},
		{"actuate_trunk", `{"which_trunk":"Front"}`, "OpenFrunk", ""},
		{"actuate_trunk", `{"which_trunk":"rear","extra":1}`, "ActuateTrunk", ""},
		{"actuate_trunk", ``, "", "invalid request body: which_trunk missing"},
		{"actuate_trunk", `{}`, "", "invalid request body: which_trunk missing"},
		{"actuate_trunk", `{"which_trunk":null}`, "", "invalid request body: which_trunk missing"},
		{"actuate_trunk", `{"which_trunk":"side"}`, "", `invalid request body: which_trunk must be "rear" or "front"`},
		{"actuate_trunk", `{"which_trunk":""}`, "", `invalid request body: which_trunk must be "rear" or "front"`},
		{"actuate_trunk", `{"which_trunk":"re ar"}`, "", `invalid request body: which_trunk must be "rear" or "front"`},
		{"actuate_trunk", `{"which_trunk":1}`, "", "invalid request body: which_trunk must be a string"},
		{"actuate_trunk", `{"which_trunk":true}`, "", "invalid request body: which_trunk must be a string"},
		{"actuate_trunk", `{"which_trunk":["rear"]}`, "", "invalid request body: which_trunk must be a string"},

		// window_control (UC1013): command required, lat and lon optional, bounded, never sent.
		{"window_control", `{"command":"vent"}`, "VentWindows", ""},
		{"window_control", `{"command":"close"}`, "CloseWindows", ""},
		{"window_control", `{"command":"close","lat":48.8566,"lon":2.3522}`, "CloseWindows", ""},
		{"window_control", `{"command":"vent","lat":0,"lon":0}`, "VentWindows", ""},
		{"window_control", `{"command":"close","lat":"-90","lon":"180"}`, "CloseWindows", ""},
		{"window_control", `{"command":"close","lat":90,"lon":-180}`, "CloseWindows", ""},
		{"window_control", `{"command":"vent","lat":12.5}`, "VentWindows", ""},
		{"window_control", `{"command":"vent","lat":null,"lon":null}`, "VentWindows", ""},
		{"window_control", `{"command":" CLOSE "}`, "CloseWindows", ""},
		{"window_control", ``, "", "invalid request body: command missing"},
		{"window_control", `{"lat":91}`, "", "invalid request body: command missing"},
		{"window_control", `{"command":"open"}`, "", `invalid request body: command must be "vent" or "close"`},
		{"window_control", `{"command":"open","lat":91}`, "", `invalid request body: command must be "vent" or "close"`},
		{"window_control", `{"command":1}`, "", "invalid request body: command must be a string"},
		{"window_control", `{"command":"vent","lat":90.5}`, "", "invalid request body: lat must be between -90 and 90 degrees"},
		{"window_control", `{"command":"vent","lat":-91}`, "", "invalid request body: lat must be between -90 and 90 degrees"},
		{"window_control", `{"command":"vent","lat":"91"}`, "", "invalid request body: lat must be between -90 and 90 degrees"},
		{"window_control", `{"command":"vent","lon":180.1}`, "", "invalid request body: lon must be between -180 and 180 degrees"},
		{"window_control", `{"command":"vent","lon":-181}`, "", "invalid request body: lon must be between -180 and 180 degrees"},
		{"window_control", `{"command":"vent","lat":91,"lon":181}`, "", "invalid request body: lat must be between -90 and 90 degrees"},
		{"window_control", `{"command":"vent","lat":"abc"}`, "", "invalid request body: lat is not a valid number"},
		{"window_control", `{"command":"vent","lat":true}`, "", "invalid request body: lat must be a number or a numeric string"},
		{"window_control", `{"command":"vent","lon":"1e999"}`, "", "invalid request body: lon is out of range"},
	}

	covered := map[string]bool{}
	for _, tt := range tests {
		covered[tt.command] = true
		t.Run(tt.command+" "+tt.body, func(t *testing.T) {
			body := decodeBody(t, tt.body)

			before := maps.Clone(body) // values are scalars: a shallow copy is enough
			validateErr := ValidateCommandBody(tt.command, body)
			if !reflect.DeepEqual(before, body) {
				t.Errorf("ValidateCommandBody modified the body: %v, was %v", body, before)
			}
			car := &fakeCar{}
			retry, runErr := fleetVehicleCommands[tt.command].run(context.Background(), car, body)
			if !reflect.DeepEqual(before, body) {
				t.Errorf("run modified the body: %v, was %v", body, before)
			}

			if tt.reason != "" {
				for name, err := range map[string]error{"ValidateCommandBody": validateErr, "run": runErr} {
					if err == nil || err.Error() != tt.reason {
						t.Errorf("%s error = %v, want %q", name, err, tt.reason)
					}
					if !errors.Is(err, ErrInvalidBody) {
						t.Errorf("%s error %v does not wrap ErrInvalidBody", name, err)
					}
				}
				if retry {
					t.Error("run asks to retry an invalid body")
				}
				if len(car.calls) != 0 {
					t.Errorf("invalid body reached the vehicle: %v", car.calls)
				}
				return
			}

			if validateErr != nil {
				t.Errorf("ValidateCommandBody error = %v, want nil", validateErr)
			}
			if runErr != nil || retry {
				t.Errorf("run = (%t, %v), want (false, nil)", retry, runErr)
			}
			if !slices.Equal(car.calls, []string{tt.call}) {
				t.Errorf("vehicle calls = %v, want [%s]", car.calls, tt.call)
			}
		})
	}

	for name := range fleetVehicleCommands {
		if !covered[name] {
			t.Errorf("registry command %q has no body case", name)
		}
	}
}

// AC7: a vehicle refusal keeps the 2.3.0 reason, is retried as in 2.3.0 and keeps the SDK error (%w).
func TestVehicleErrorsKeep230Messages(t *testing.T) {
	tests := []struct {
		command string
		body    string
		want    string
	}{
		{"auto_conditioning_start", "", "failed to start auto conditioning: " + vehicleFault},
		{"auto_conditioning_stop", "", "failed to stop auto conditioning: " + vehicleFault},
		{"charge_port_door_open", "", "failed to open charge port: " + vehicleFault},
		{"charge_port_door_close", "", "failed to close charge port: " + vehicleFault},
		{"flash_lights", "", "failed to flash lights: " + vehicleFault},
		{"wake_up", "", "failed to wake up car: " + vehicleFault},
		{"honk_horn", "", "failed to honk horn " + vehicleFault},
		{"door_lock", "", "failed to lock " + vehicleFault},
		{"door_unlock", "", "failed to unlock " + vehicleFault},
		{"set_sentry_mode", `{"on":true}`, "failed to set sentry mode " + vehicleFault},
		{"charge_start", "", "failed to start charge: " + vehicleFault},
		{"charge_stop", "", "failed to stop charge: " + vehicleFault},
		{"set_charging_amps", `{"charging_amps":"16"}`, "failed to set charging Amps to 16: " + vehicleFault},
		{"set_charge_limit", `{"percent":80}`, "failed to set charge limit to 80 %: " + vehicleFault},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) { assertVehicleError(t, tt.command, tt.body, tt.want) })
	}
}

// UC1010: the added commands report the vehicle refusal, retried, with the SDK error wrapped.
func TestAddedCommandsVehicleErrors(t *testing.T) {
	tests := []struct {
		command string
		body    string
		want    string
	}{
		{"set_temps", `{"driver_temp":21}`, "failed to set temps to 21.0/21.0: " + vehicleFault},
		{"set_temps", `{"driver_temp":21.5,"passenger_temp":"19"}`, "failed to set temps to 21.5/19.0: " + vehicleFault},
		{"set_preconditioning_max", `{"on":true}`, "failed to set preconditioning max: " + vehicleFault},
		{"set_climate_keeper_mode", `{"climate_keeper_mode":2}`, "failed to set climate keeper mode to 2: " + vehicleFault},
		{"set_cabin_overheat_protection", `{"on":true}`, "failed to set cabin overheat protection: " + vehicleFault},
		{"set_cop_temp", `{"cop_temp":"1"}`, "failed to set cabin overheat protection temperature to cop_temp 1: " + vehicleFault},
		{"set_bioweapon_mode", `{"on":true}`, "failed to set bioweapon mode: " + vehicleFault},
		{"remote_seat_heater_request", `{"heater":"2","level":1}`, "failed to set seat heater 2 to level 1: " + vehicleFault},
		{"remote_seat_cooler_request", `{"seat_position":2,"seat_cooler_level":"3"}`, "failed to set seat cooler at seat_position 2 to level 3: " + vehicleFault},
		{"remote_auto_seat_climate_request", `{"auto_seat_position":1,"auto_climate_on":true}`, "failed to set auto seat climate at auto_seat_position 1: " + vehicleFault},
		{"remote_steering_wheel_heater_request", `{"on":true}`, "failed to set steering wheel heater: " + vehicleFault},
		{"actuate_trunk", `{"which_trunk":"front"}`, "failed to open frunk: " + vehicleFault},
		{"window_control", `{"command":"vent"}`, "failed to vent windows: " + vehicleFault},
		{"window_control", `{"command":"close","lat":1,"lon":2}`, "failed to close windows: " + vehicleFault},
	}
	for _, tt := range tests {
		t.Run(tt.command+" "+tt.body, func(t *testing.T) { assertVehicleError(t, tt.command, tt.body, tt.want) })
	}
}

const vehicleFault = "MESSAGEFAULT_ERROR_INSUFFICIENT_PRIVILEGES"

// assertVehicleError runs a command against a car that refuses it with an insufficient
// privileges fault: the error must read want, be retried and wrap the SDK error.
func assertVehicleError(t *testing.T, command, body, want string) {
	t.Helper()
	sdkErr := &protocol.RoutableMessageError{Code: universalmessage.MessageFault_E_MESSAGEFAULT_ERROR_INSUFFICIENT_PRIVILEGES}
	car := &fakeCar{err: sdkErr}
	retry, err := fleetVehicleCommands[command].run(context.Background(), car, decodeBody(t, body))
	if err == nil || err.Error() != want {
		t.Fatalf("run error = %v, want %q", err, want)
	}
	if !retry {
		t.Error("run does not retry a vehicle error (2.3.0 retries it)")
	}
	var routable *protocol.RoutableMessageError
	if !errors.As(err, &routable) {
		t.Errorf("run error %v does not wrap the SDK error", err)
	}
}

// optFloatArg refuses NaN and infinities on the number branch too (unreachable through JSON).
func TestOptFloatArgRefusesNonFinite(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, present, err := commandArgs{"k": v}.optFloatArg("k")
		if err == nil || err.Error() != "invalid request body: k is not a valid number" || !present {
			t.Errorf("optFloatArg(%v) = present %t, err %v", v, present, err)
		}
		if _, _, err := (commandArgs{"driver_temp": v}).cabinTemps(); err == nil {
			t.Errorf("cabinTemps accepted %v", v)
		}
	}
	if err := checkCabinTemp("driver_temp", math.NaN()); err == nil {
		t.Error("checkCabinTemp accepted NaN")
	}
}

// UC1011 AC1: the Fleet index tables match the SDK and protobuf constants.
func TestFleetEnumTablesMatchSDK(t *testing.T) {
	keeper := []carserver.HvacClimateKeeperAction_ClimateKeeperAction_E{
		carserver.HvacClimateKeeperAction_ClimateKeeperAction_Off,
		carserver.HvacClimateKeeperAction_ClimateKeeperAction_On,
		carserver.HvacClimateKeeperAction_ClimateKeeperAction_Dog,
		carserver.HvacClimateKeeperAction_ClimateKeeperAction_Camp,
	}
	if len(climateKeeperModes) != len(keeper) {
		t.Fatalf("climateKeeperModes has %d entries, want %d", len(climateKeeperModes), len(keeper))
	}
	for i, want := range keeper {
		if climateKeeperModes[i] != want {
			t.Errorf("climate_keeper_mode %d = %v, want %v", i, climateKeeperModes[i], want)
		}
	}
	cop := []carserver.ClimateState_CopActivationTemp{
		carserver.ClimateState_CopActivationTempLow,
		carserver.ClimateState_CopActivationTempMedium,
		carserver.ClimateState_CopActivationTempHigh,
	}
	if len(copActivationLevels) != len(cop) {
		t.Fatalf("copActivationLevels has %d entries, want %d", len(copActivationLevels), len(cop))
	}
	for i, want := range cop {
		if got := carserver.ClimateState_CopActivationTemp(copActivationLevels[i]); got != want {
			t.Errorf("cop_temp %d = %v, want %v", i, got, want)
		}
	}
}

// enumArg refuses NaN as a fraction and falls back to out of range for infinities
// (unreachable through JSON).
func TestEnumArgRefusesNonFinite(t *testing.T) {
	tests := []struct {
		v    float64
		want string
	}{
		{math.NaN(), "invalid request body: k must be an integer between 0 and 2"},
		{math.Inf(1), "invalid request body: k is out of range"},
		{math.Inf(-1), "invalid request body: k is out of range"},
	}
	for _, tt := range tests {
		if _, err := (commandArgs{"k": tt.v}).enumArg("k", 3); err == nil || err.Error() != tt.want {
			t.Errorf("enumArg(%v) error = %v, want %q", tt.v, err, tt.want)
		}
	}
}

// UC1012 AC1: the Fleet seat tables match the SDK and protobuf constants.
func TestFleetSeatTablesMatchSDK(t *testing.T) {
	heaters := []vehicle.SeatPosition{
		vehicle.SeatFrontLeft, vehicle.SeatFrontRight, vehicle.SeatSecondRowLeft, vehicle.SeatSecondRowLeftBack,
		vehicle.SeatSecondRowCenter, vehicle.SeatSecondRowRight, vehicle.SeatSecondRowRightBack,
		vehicle.SeatThirdRowLeft, vehicle.SeatThirdRowRight,
	}
	if len(seatHeaterPositions) != len(heaters) {
		t.Fatalf("seatHeaterPositions has %d entries, want %d", len(seatHeaterPositions), len(heaters))
	}
	for i, want := range heaters {
		if seatHeaterPositions[i] != want {
			t.Errorf("heater %d = %s, want %s", i, sdkSeatName(seatHeaterPositions[i]), sdkSeatName(want))
		}
		if seatHeaterPositions[i] == vehicle.SeatUnknown {
			t.Errorf("heater %d maps to SeatUnknown", i)
		}
	}

	levels := []carserver.HvacSeatCoolerActions_HvacSeatCoolerLevel_E{
		carserver.HvacSeatCoolerActions_HvacSeatCoolerLevel_Off,
		carserver.HvacSeatCoolerActions_HvacSeatCoolerLevel_Low,
		carserver.HvacSeatCoolerActions_HvacSeatCoolerLevel_Med,
		carserver.HvacSeatCoolerActions_HvacSeatCoolerLevel_High,
	}
	if len(seatLevels) != len(levels) {
		t.Fatalf("seatLevels has %d entries, want %d", len(seatLevels), len(levels))
	}
	for i, want := range levels {
		// The SDK adds one to the level (climate.go).
		if got := carserver.HvacSeatCoolerActions_HvacSeatCoolerLevel_E(seatLevels[i] + 1); got != want {
			t.Errorf("level %d = %v, want %v", i, got, want)
		}
	}

	// frontSeats is indexed by the Fleet value, which is the protobuf value; index 0 is unused.
	if len(frontSeats) != 3 {
		t.Fatalf("frontSeats has %d entries, want 3 (frontSeatArg accepts 1 and 2)", len(frontSeats))
	}
	if frontSeats[1] != vehicle.SeatFrontLeft || frontSeats[2] != vehicle.SeatFrontRight {
		t.Errorf("frontSeats = %s, %s", sdkSeatName(frontSeats[1]), sdkSeatName(frontSeats[2]))
	}
	if carserver.HvacSeatCoolerActions_HvacSeatCoolerPosition_FrontLeft != 1 ||
		carserver.HvacSeatCoolerActions_HvacSeatCoolerPosition_FrontRight != 2 ||
		carserver.AutoSeatClimateAction_AutoSeatPosition_FrontLeft != 1 ||
		carserver.AutoSeatClimateAction_AutoSeatPosition_FrontRight != 2 {
		t.Error("the protobuf front seat values are no longer 1 and 2")
	}
}

// intRangeArg: bounds, fractions (message built with lo and hi) and non-finite values.
func TestIntRangeArg(t *testing.T) {
	tests := []struct {
		name   string
		v      interface{}
		lo, hi int
		want   int
		reason string
	}{
		{"in range", 2.0, 1, 2, 2, ""},
		{"lower bound string", "1", 1, 2, 1, ""},
		{"zero below lo", 0.0, 1, 2, 0, "k must be an integer between 1 and 2"},
		{"zero string below lo", "0", 1, 2, 0, "k must be an integer between 1 and 2"},
		{"above hi", 3.0, 1, 2, 0, "k must be an integer between 1 and 2"},
		{"fraction", 1.5, 1, 2, 0, "k must be an integer between 1 and 2"},
		{"NaN", math.NaN(), 1, 2, 0, "k must be an integer between 1 and 2"},
		{"+Inf", math.Inf(1), 1, 2, 0, "k is out of range"},
		{"-Inf", math.Inf(-1), 0, 8, 0, "k is out of range"},
		{"not a number", "x", 1, 2, 0, "k is not a valid integer"},
		{"bool", true, 1, 2, 0, "k must be a number or a numeric string"},
		{"missing", nil, 1, 2, 0, "k missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (commandArgs{"k": tt.v}).intRangeArg("k", tt.lo, tt.hi)
			if tt.reason == "" {
				if err != nil || got != tt.want {
					t.Errorf("intRangeArg = %d, %v, want %d", got, err, tt.want)
				}
				return
			}
			if err == nil || err.Error() != "invalid request body: "+tt.reason {
				t.Errorf("intRangeArg error = %v, want %q", err, tt.reason)
			}
		})
	}
}

// AC6: "already charging", "charging complete" and "not charging" stay successes.
func TestAlreadyDoneErrorsAreSuccess(t *testing.T) {
	vehicleRefusal := func(reason string) error {
		return &protocol.NominalError{Details: protocol.NewError("car could not execute command: "+reason, false, false)}
	}
	tests := []struct {
		command   string
		refusal   string
		wantError bool
	}{
		{"charge_start", "is_charging", false},
		{"charge_start", "complete", false},
		{"charge_start", "not_charging", true},
		{"charge_stop", "not_charging", false},
		{"charge_stop", "is_charging", true},
	}
	for _, tt := range tests {
		t.Run(tt.command+" "+tt.refusal, func(t *testing.T) {
			car := &fakeCar{err: vehicleRefusal(tt.refusal)}
			retry, err := fleetVehicleCommands[tt.command].run(context.Background(), car, nil)
			if !tt.wantError {
				if err != nil || retry {
					t.Errorf("run = (%t, %v), want (false, nil)", retry, err)
				}
				return
			}
			if err == nil || !retry || !strings.Contains(err.Error(), tt.refusal) {
				t.Errorf("run = (%t, %v), want a retried error containing %q", retry, err, tt.refusal)
			}
		})
	}
}

func TestFleetCommandNamesAreTheRegistry(t *testing.T) {
	names := FleetCommandNames()

	if want := slices.Sorted(maps.Keys(fleetVehicleCommands)); !slices.Equal(names, want) {
		t.Fatalf("FleetCommandNames() = %v, want %v", names, want)
	}
	if !slices.IsSorted(names) {
		t.Errorf("names are not sorted: %v", names)
	}
	if len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Errorf("names contain duplicates: %v", names)
	}
	for _, name := range names {
		if fleetVehicleCommands[name].execute == nil {
			t.Errorf("command %q has no execute function", name)
		}
		if !IsSupportedCommand(name) {
			t.Errorf("command %q is announced but not supported", name)
		}
		if slices.Contains(legacyRouteCommands, name) {
			t.Errorf("legacy route command %q must not be announced", name)
		}
	}

	// The result is a fresh copy.
	names[0] = "changed"
	if again := FleetCommandNames(); again[0] == "changed" {
		t.Errorf("modifying the returned slice altered the next call")
	}
}

// The API only grows: removing one of these commands must be a deliberate change of this test.
func TestFleetCommandNamesFloor(t *testing.T) {
	floor := []string{
		"auto_conditioning_start", "auto_conditioning_stop", "charge_port_door_close", "charge_port_door_open",
		"charge_start", "charge_stop", "door_lock", "door_unlock", "flash_lights", "honk_horn",
		"set_charge_limit", "set_charging_amps", "set_sentry_mode", "wake_up",
		"set_preconditioning_max", "set_temps",
		"set_climate_keeper_mode", "set_cabin_overheat_protection", "set_cop_temp", "set_bioweapon_mode",
		"remote_seat_heater_request", "remote_seat_cooler_request", "remote_auto_seat_climate_request", "remote_steering_wheel_heater_request",
		"actuate_trunk", "window_control",
	}
	names := FleetCommandNames()
	if len(names) < 26 {
		t.Errorf("FleetCommandNames() has %d names, want at least 26", len(names))
	}
	for _, name := range floor {
		if !slices.Contains(names, name) {
			t.Errorf("command %q is missing from FleetCommandNames()", name)
		}
	}
}

// UC1013: actuate_trunk toggles, so a rear failure is not retried; front and the idempotent
// commands keep the retries. Only actuate_trunk carries notRetried.
func TestToggleVehicleErrorNotRetried(t *testing.T) {
	for name, handler := range fleetVehicleCommands {
		if (handler.notRetried != nil) != (name == "actuate_trunk") {
			t.Errorf("command %q: notRetried set = %t", name, handler.notRetried != nil)
		}
	}
	tests := []struct {
		body      string
		wantRetry bool
		want      string
	}{
		{`{"which_trunk":"rear"}`, false, "failed to actuate trunk (not retried, it is a toggle): " + vehicleFault},
		{`{"which_trunk":" Rear "}`, false, "failed to actuate trunk (not retried, it is a toggle): " + vehicleFault},
		{`{"which_trunk":"front"}`, true, "failed to open frunk: " + vehicleFault},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			sdkErr := &protocol.RoutableMessageError{Code: universalmessage.MessageFault_E_MESSAGEFAULT_ERROR_INSUFFICIENT_PRIVILEGES}
			car := &fakeCar{err: sdkErr}
			retry, err := fleetVehicleCommands["actuate_trunk"].run(context.Background(), car, decodeBody(t, tt.body))
			if err == nil || err.Error() != tt.want {
				t.Fatalf("run error = %v, want %q", err, tt.want)
			}
			if retry != tt.wantRetry {
				t.Errorf("retry = %t, want %t", retry, tt.wantRetry)
			}
			var routable *protocol.RoutableMessageError
			if !errors.As(err, &routable) {
				t.Errorf("run error %v does not wrap the SDK error", err)
			}
			if len(car.calls) != 1 {
				t.Errorf("vehicle calls = %v, want exactly one", car.calls)
			}
		})
	}
}

// The predicate is fail-safe: anything but "front" counts as a toggle.
func TestActuateTrunkNotRetriedPredicate(t *testing.T) {
	notRetried := fleetVehicleCommands["actuate_trunk"].notRetried
	tests := []struct {
		args commandArgs
		want bool
	}{
		{commandArgs{"which_trunk": "rear"}, true},
		{commandArgs{"which_trunk": "front"}, false},
		{commandArgs{"which_trunk": " FRONT "}, false},
		{commandArgs{"which_trunk": "side"}, true},
		{commandArgs{}, true},
		{nil, true},
	}
	for _, tt := range tests {
		if got := notRetried(tt.args); got != tt.want {
			t.Errorf("notRetried(%v) = %t, want %t", tt.args, got, tt.want)
		}
	}
}

func TestChoiceArg(t *testing.T) {
	tests := []struct {
		name   string
		args   commandArgs
		want   string
		reason string
	}{
		{"canonical", commandArgs{"k": "rear"}, "rear", ""},
		{"normalized", commandArgs{"k": "\t Front \n"}, "front", ""},
		{"absent", commandArgs{}, "", "k missing"},
		{"null", commandArgs{"k": nil}, "", "k missing"},
		{"number", commandArgs{"k": 1.0}, "", "k must be a string"},
		{"bool", commandArgs{"k": true}, "", "k must be a string"},
		{"empty", commandArgs{"k": ""}, "", `k must be "rear" or "front"`},
		{"unknown", commandArgs{"k": "side"}, "", `k must be "rear" or "front"`},
		{"long s is not folded", commandArgs{"k": "frontſ"}, "", `k must be "rear" or "front"`},
		{"kelvin sign is not folded", commandArgs{"k": "Kear"}, "", `k must be "rear" or "front"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.args.choiceArg("k", trunkChoices...)
			if tt.reason == "" {
				if err != nil || got != tt.want {
					t.Errorf("choiceArg = %q, %v, want %q", got, err, tt.want)
				}
				return
			}
			if err == nil || err.Error() != "invalid request body: "+tt.reason || !errors.Is(err, ErrInvalidBody) {
				t.Errorf("choiceArg error = %v, want %q", err, tt.reason)
			}
		})
	}
}

func TestOptCoordinateArg(t *testing.T) {
	tests := []struct {
		name   string
		v      interface{}
		limit  int
		reason string
	}{
		{"absent", nil, 90, ""},
		{"zero", 0.0, 90, ""},
		{"upper bound", 90.0, 90, ""},
		{"lower bound", -90.0, 90, ""},
		{"string bound", "180", 180, ""},
		{"above", 90.0000001, 90, "k must be between -90 and 90 degrees"},
		{"below", -180.5, 180, "k must be between -180 and 180 degrees"},
		{"string above", "91", 90, "k must be between -90 and 90 degrees"},
		{"NaN", math.NaN(), 90, "k is not a valid number"},
		{"+Inf", math.Inf(1), 90, "k is not a valid number"},
		{"-Inf", math.Inf(-1), 90, "k is not a valid number"},
		{"bool", true, 90, "k must be a number or a numeric string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (commandArgs{"k": tt.v}).optCoordinateArg("k", tt.limit)
			if tt.reason == "" {
				if err != nil {
					t.Errorf("optCoordinateArg error = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != "invalid request body: "+tt.reason {
				t.Errorf("optCoordinateArg error = %v, want %q", err, tt.reason)
			}
		})
	}
}
