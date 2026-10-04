package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const closuresZero = `{"timestamp":0,"door_open_driver_front":false,"door_open_driver_rear":false,` +
	`"door_open_passenger_front":false,"door_open_passenger_rear":false,"door_open_trunk_front":false,` +
	`"door_open_trunk_rear":false,"window_open_driver_front":false,"window_open_passenger_front":false,` +
	`"window_open_driver_rear":false,"window_open_passenger_rear":false,"sun_roof_state":"\u003cnil\u003e",` +
	`"sun_roof_percent_open":0,"locked":false,"is_user_present":false,"valet_mode":false,` +
	`"sentry_mode_state":"\u003cnil\u003e","sentry_mode":false,"tonneau_state":"\u003cnil\u003e","tonneau_percent_open":0,` +
	`"tonneau_in_motion":false}`

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func closuresJSON(t *testing.T, c *carserver.ClosuresState) string {
	t.Helper()
	return mustJSON(t, ClosuresStateFromBle(&carserver.VehicleData{ClosuresState: c}))
}

// AC1: a hand-built message is converted with every expected key, in order.
func TestClosuresStateJSON(t *testing.T) {
	c := &carserver.ClosuresState{
		OptionalDoorOpenPassengerFront: &carserver.ClosuresState_DoorOpenPassengerFront{DoorOpenPassengerFront: true},
		OptionalDoorOpenTrunkRear:      &carserver.ClosuresState_DoorOpenTrunkRear{DoorOpenTrunkRear: true},
		SunRoofState:                   &carserver.ClosuresState_SunRoofState{Type: &carserver.ClosuresState_SunRoofState_Vent{Vent: &carserver.Void{}}},
		OptionalSunRoofPercentOpen:     &carserver.ClosuresState_SunRoofPercentOpen{SunRoofPercentOpen: 15},
		OptionalLocked:                 &carserver.ClosuresState_Locked{Locked: true},
		SentryModeState:                &carserver.ClosuresState_SentryModeState{Type: &carserver.ClosuresState_SentryModeState_Armed{Armed: &carserver.Void{}}},
		OptionalTonneauState:           &carserver.ClosuresState_TonneauState{TonneauState: vcsec.ClosureState_E_CLOSURESTATE_AJAR},
		Timestamp:                      &timestamppb.Timestamp{Seconds: 1767254400},
	}
	want := `{"timestamp":1767254400,"door_open_driver_front":false,"door_open_driver_rear":false,` +
		`"door_open_passenger_front":true,"door_open_passenger_rear":false,"door_open_trunk_front":false,` +
		`"door_open_trunk_rear":true,"window_open_driver_front":false,"window_open_passenger_front":false,` +
		`"window_open_driver_rear":false,"window_open_passenger_rear":false,"sun_roof_state":"Vent",` +
		`"sun_roof_percent_open":15,"locked":true,"is_user_present":false,"valet_mode":false,` +
		`"sentry_mode_state":"Armed","sentry_mode":true,"tonneau_state":"CLOSURESTATE_AJAR",` +
		`"tonneau_percent_open":0,"tonneau_in_motion":false}`
	if got := closuresJSON(t, c); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Each of the 14 booleans maps to its own key: set one at a time, exactly that key is true.
func TestClosuresStateFieldMapping(t *testing.T) {
	setters := map[string]func(*carserver.ClosuresState){
		"door_open_driver_front": func(c *carserver.ClosuresState) {
			c.OptionalDoorOpenDriverFront = &carserver.ClosuresState_DoorOpenDriverFront{DoorOpenDriverFront: true}
		},
		"door_open_driver_rear": func(c *carserver.ClosuresState) {
			c.OptionalDoorOpenDriverRear = &carserver.ClosuresState_DoorOpenDriverRear{DoorOpenDriverRear: true}
		},
		"door_open_passenger_front": func(c *carserver.ClosuresState) {
			c.OptionalDoorOpenPassengerFront = &carserver.ClosuresState_DoorOpenPassengerFront{DoorOpenPassengerFront: true}
		},
		"door_open_passenger_rear": func(c *carserver.ClosuresState) {
			c.OptionalDoorOpenPassengerRear = &carserver.ClosuresState_DoorOpenPassengerRear{DoorOpenPassengerRear: true}
		},
		"door_open_trunk_front": func(c *carserver.ClosuresState) {
			c.OptionalDoorOpenTrunkFront = &carserver.ClosuresState_DoorOpenTrunkFront{DoorOpenTrunkFront: true}
		},
		"door_open_trunk_rear": func(c *carserver.ClosuresState) {
			c.OptionalDoorOpenTrunkRear = &carserver.ClosuresState_DoorOpenTrunkRear{DoorOpenTrunkRear: true}
		},
		"window_open_driver_front": func(c *carserver.ClosuresState) {
			c.OptionalWindowOpenDriverFront = &carserver.ClosuresState_WindowOpenDriverFront{WindowOpenDriverFront: true}
		},
		"window_open_passenger_front": func(c *carserver.ClosuresState) {
			c.OptionalWindowOpenPassengerFront = &carserver.ClosuresState_WindowOpenPassengerFront{WindowOpenPassengerFront: true}
		},
		"window_open_driver_rear": func(c *carserver.ClosuresState) {
			c.OptionalWindowOpenDriverRear = &carserver.ClosuresState_WindowOpenDriverRear{WindowOpenDriverRear: true}
		},
		"window_open_passenger_rear": func(c *carserver.ClosuresState) {
			c.OptionalWindowOpenPassengerRear = &carserver.ClosuresState_WindowOpenPassengerRear{WindowOpenPassengerRear: true}
		},
		"locked": func(c *carserver.ClosuresState) {
			c.OptionalLocked = &carserver.ClosuresState_Locked{Locked: true}
		},
		"is_user_present": func(c *carserver.ClosuresState) {
			c.OptionalIsUserPresent = &carserver.ClosuresState_IsUserPresent{IsUserPresent: true}
		},
		"valet_mode": func(c *carserver.ClosuresState) {
			c.OptionalValetMode = &carserver.ClosuresState_ValetMode{ValetMode: true}
		},
		"tonneau_in_motion": func(c *carserver.ClosuresState) {
			c.OptionalTonneauInMotion = &carserver.ClosuresState_TonneauInMotion{TonneauInMotion: true}
		},
	}
	if len(setters) != 14 {
		t.Fatalf("%d setters, want 14", len(setters))
	}
	for key, set := range setters {
		t.Run(key, func(t *testing.T) {
			c := &carserver.ClosuresState{}
			set(c)
			var got map[string]any
			if err := json.Unmarshal([]byte(closuresJSON(t, c)), &got); err != nil {
				t.Fatal(err)
			}
			bools := 0
			for k, v := range got {
				if b, isBool := v.(bool); isBool {
					bools++
					if b != (k == key) {
						t.Errorf("%s = %v with only %s set", k, b, key)
					}
				}
			}
			// 14 booleans plus sentry_mode.
			if bools != 15 {
				t.Errorf("%d boolean keys, want 15", bools)
			}
		})
	}
}

// AC2: Armed, Aware, Panic and Quiet give sentry_mode=true; Off, Idle, an empty message and no message give false.
func TestClosuresStateSentryMode(t *testing.T) {
	v := &carserver.Void{}
	tests := []struct {
		name    string
		state   *carserver.ClosuresState_SentryModeState
		wantStr string
		wantOn  bool
	}{
		{"Off", &carserver.ClosuresState_SentryModeState{Type: &carserver.ClosuresState_SentryModeState_Off{Off: v}}, "Off", false},
		{"Idle", &carserver.ClosuresState_SentryModeState{Type: &carserver.ClosuresState_SentryModeState_Idle{Idle: v}}, "Idle", false},
		{"Armed", &carserver.ClosuresState_SentryModeState{Type: &carserver.ClosuresState_SentryModeState_Armed{Armed: v}}, "Armed", true},
		{"Aware", &carserver.ClosuresState_SentryModeState{Type: &carserver.ClosuresState_SentryModeState_Aware{Aware: v}}, "Aware", true},
		{"Panic", &carserver.ClosuresState_SentryModeState{Type: &carserver.ClosuresState_SentryModeState_Panic{Panic: v}}, "Panic", true},
		{"Quiet", &carserver.ClosuresState_SentryModeState{Type: &carserver.ClosuresState_SentryModeState_Quiet{Quiet: v}}, "Quiet", true},
		{"empty message", &carserver.ClosuresState_SentryModeState{}, "", false},
		{"absent", nil, "<nil>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClosuresStateFromBle(&carserver.VehicleData{ClosuresState: &carserver.ClosuresState{SentryModeState: tt.state}})
			if got.SentryModeState != tt.wantStr || got.SentryMode != tt.wantOn {
				t.Errorf("sentry_mode_state=%q sentry_mode=%v, want %q %v", got.SentryModeState, got.SentryMode, tt.wantStr, tt.wantOn)
			}
			if sentryModeOn(tt.state) != tt.wantOn {
				t.Errorf("sentryModeOn = %v, want %v", !tt.wantOn, tt.wantOn)
			}
		})
	}
}

// AC3: explicit false values are served, not omitted.
func TestClosuresStateAllClosed(t *testing.T) {
	c := &carserver.ClosuresState{
		OptionalDoorOpenDriverFront:      &carserver.ClosuresState_DoorOpenDriverFront{},
		OptionalDoorOpenDriverRear:       &carserver.ClosuresState_DoorOpenDriverRear{},
		OptionalDoorOpenPassengerFront:   &carserver.ClosuresState_DoorOpenPassengerFront{},
		OptionalDoorOpenPassengerRear:    &carserver.ClosuresState_DoorOpenPassengerRear{},
		OptionalDoorOpenTrunkFront:       &carserver.ClosuresState_DoorOpenTrunkFront{},
		OptionalDoorOpenTrunkRear:        &carserver.ClosuresState_DoorOpenTrunkRear{},
		OptionalWindowOpenDriverFront:    &carserver.ClosuresState_WindowOpenDriverFront{},
		OptionalWindowOpenPassengerFront: &carserver.ClosuresState_WindowOpenPassengerFront{},
		OptionalWindowOpenDriverRear:     &carserver.ClosuresState_WindowOpenDriverRear{},
		OptionalWindowOpenPassengerRear:  &carserver.ClosuresState_WindowOpenPassengerRear{},
		OptionalLocked:                   &carserver.ClosuresState_Locked{},
		OptionalIsUserPresent:            &carserver.ClosuresState_IsUserPresent{},
		OptionalValetMode:                &carserver.ClosuresState_ValetMode{},
		OptionalTonneauInMotion:          &carserver.ClosuresState_TonneauInMotion{},
	}
	got := closuresJSON(t, c)
	for _, key := range []string{
		"door_open_driver_front", "door_open_driver_rear", "door_open_passenger_front", "door_open_passenger_rear",
		"door_open_trunk_front", "door_open_trunk_rear", "window_open_driver_front", "window_open_passenger_front",
		"window_open_driver_rear", "window_open_passenger_rear", "locked", "is_user_present", "valet_mode", "tonneau_in_motion",
	} {
		if !strings.Contains(got, `"`+key+`":false`) {
			t.Errorf("%s missing or not false in %s", key, got)
		}
	}
	if strings.Contains(got, ":true") {
		t.Errorf("a field is true in %s", got)
	}
}

func TestClosuresStateZeroValues(t *testing.T) {
	for name, got := range map[string]string{
		"nil VehicleData":     mustJSON(t, ClosuresStateFromBle(nil)),
		"empty VehicleData":   mustJSON(t, ClosuresStateFromBle(&carserver.VehicleData{})),
		"empty ClosuresState": closuresJSON(t, &carserver.ClosuresState{}),
	} {
		if got != closuresZero {
			t.Errorf("%s: got %s\nwant %s", name, got, closuresZero)
		}
	}

	typ := reflect.TypeOf(ClosuresState{})
	if typ.NumField() != 21 {
		t.Errorf("%d fields, want 21", typ.NumField())
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(closuresZero), &keys); err != nil || len(keys) != 21 {
		t.Errorf("%d keys (%v), want 21", len(keys), err)
	}
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || strings.Contains(tag, "omitempty") {
			t.Errorf("field %s: json tag %q must be set without omitempty", typ.Field(i).Name, tag)
		}
	}
}

func TestClosuresStateEnumEdgeCases(t *testing.T) {
	t.Run("empty sun roof message", func(t *testing.T) {
		got := ClosuresStateFromBle(&carserver.VehicleData{ClosuresState: &carserver.ClosuresState{SunRoofState: &carserver.ClosuresState_SunRoofState{}}})
		if got.SunRoofState != "" {
			t.Errorf("sun_roof_state = %q, want empty", got.SunRoofState)
		}
	})
	t.Run("explicit tonneau CLOSED", func(t *testing.T) {
		got := ClosuresStateFromBle(&carserver.VehicleData{ClosuresState: &carserver.ClosuresState{
			OptionalTonneauState: &carserver.ClosuresState_TonneauState{TonneauState: vcsec.ClosureState_E_CLOSURESTATE_CLOSED},
		}})
		if got.TonneauState != "CLOSURESTATE_CLOSED" {
			t.Errorf("tonneau_state = %q, want CLOSURESTATE_CLOSED", got.TonneauState)
		}
	})
}
