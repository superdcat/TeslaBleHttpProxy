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
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/universalmessage"
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
	}
	names := FleetCommandNames()
	for _, name := range floor {
		if !slices.Contains(names, name) {
			t.Errorf("command %q is missing from FleetCommandNames()", name)
		}
	}
}
