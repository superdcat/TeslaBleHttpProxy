package models

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const tirePressureZero = `{"timestamp":0,"tpms_pressure_fl":0,"tpms_pressure_fr":0,"tpms_pressure_rl":0,"tpms_pressure_rr":0,` +
	`"tpms_last_seen_pressure_time_fl":0,"tpms_last_seen_pressure_time_fr":0,"tpms_last_seen_pressure_time_rl":0,` +
	`"tpms_last_seen_pressure_time_rr":0,"tpms_hard_warning_fl":false,"tpms_hard_warning_fr":false,` +
	`"tpms_hard_warning_rl":false,"tpms_hard_warning_rr":false,"tpms_soft_warning_fl":false,"tpms_soft_warning_fr":false,` +
	`"tpms_soft_warning_rl":false,"tpms_soft_warning_rr":false,"tpms_rcp_front_value":0,"tpms_rcp_rear_value":0}`

func tirePressureJSON(t *testing.T, s *carserver.TirePressureState) string {
	t.Helper()
	return mustJSON(t, TirePressureFromBle(&carserver.VehicleData{TirePressureState: s}))
}

// AC1: a hand-built message is converted with every expected key, in order, pressures in bar.
func TestTirePressureJSON(t *testing.T) {
	s := &carserver.TirePressureState{
		Timestamp:                  &timestamppb.Timestamp{Seconds: 1767254400},
		OptionalTpmsPressureFl:     &carserver.TirePressureState_TpmsPressureFl{TpmsPressureFl: 2.9},
		OptionalTpmsPressureFr:     &carserver.TirePressureState_TpmsPressureFr{TpmsPressureFr: 2.925},
		OptionalTpmsPressureRl:     &carserver.TirePressureState_TpmsPressureRl{TpmsPressureRl: 2.1},
		OptionalTpmsPressureRr:     &carserver.TirePressureState_TpmsPressureRr{TpmsPressureRr: 1.5},
		TpmsLastSeenPressureTimeFl: &timestamppb.Timestamp{Seconds: 1767254100},
		OptionalTpmsHardWarningRr:  &carserver.TirePressureState_TpmsHardWarningRr{TpmsHardWarningRr: true},
		OptionalTpmsSoftWarningRl:  &carserver.TirePressureState_TpmsSoftWarningRl{TpmsSoftWarningRl: true},
		OptionalTpmsRcpFrontValue:  &carserver.TirePressureState_TpmsRcpFrontValue{TpmsRcpFrontValue: 2.95},
		OptionalTpmsRcpRearValue:   &carserver.TirePressureState_TpmsRcpRearValue{TpmsRcpRearValue: 3.1},
	}
	want := `{"timestamp":1767254400,"tpms_pressure_fl":2.9,"tpms_pressure_fr":2.925,"tpms_pressure_rl":2.1,` +
		`"tpms_pressure_rr":1.5,"tpms_last_seen_pressure_time_fl":1767254100,"tpms_last_seen_pressure_time_fr":0,` +
		`"tpms_last_seen_pressure_time_rl":0,"tpms_last_seen_pressure_time_rr":0,"tpms_hard_warning_fl":false,` +
		`"tpms_hard_warning_fr":false,"tpms_hard_warning_rl":false,"tpms_hard_warning_rr":true,` +
		`"tpms_soft_warning_fl":false,"tpms_soft_warning_fr":false,"tpms_soft_warning_rl":true,` +
		`"tpms_soft_warning_rr":false,"tpms_rcp_front_value":2.95,"tpms_rcp_rear_value":3.1}`
	if got := tirePressureJSON(t, s); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Each of the 8 warnings maps to its own key; pressures and last-seen times map to their own wheel.
func TestTirePressureFieldMapping(t *testing.T) {
	setters := map[string]func(*carserver.TirePressureState){
		"tpms_hard_warning_fl": func(s *carserver.TirePressureState) {
			s.OptionalTpmsHardWarningFl = &carserver.TirePressureState_TpmsHardWarningFl{TpmsHardWarningFl: true}
		},
		"tpms_hard_warning_fr": func(s *carserver.TirePressureState) {
			s.OptionalTpmsHardWarningFr = &carserver.TirePressureState_TpmsHardWarningFr{TpmsHardWarningFr: true}
		},
		"tpms_hard_warning_rl": func(s *carserver.TirePressureState) {
			s.OptionalTpmsHardWarningRl = &carserver.TirePressureState_TpmsHardWarningRl{TpmsHardWarningRl: true}
		},
		"tpms_hard_warning_rr": func(s *carserver.TirePressureState) {
			s.OptionalTpmsHardWarningRr = &carserver.TirePressureState_TpmsHardWarningRr{TpmsHardWarningRr: true}
		},
		"tpms_soft_warning_fl": func(s *carserver.TirePressureState) {
			s.OptionalTpmsSoftWarningFl = &carserver.TirePressureState_TpmsSoftWarningFl{TpmsSoftWarningFl: true}
		},
		"tpms_soft_warning_fr": func(s *carserver.TirePressureState) {
			s.OptionalTpmsSoftWarningFr = &carserver.TirePressureState_TpmsSoftWarningFr{TpmsSoftWarningFr: true}
		},
		"tpms_soft_warning_rl": func(s *carserver.TirePressureState) {
			s.OptionalTpmsSoftWarningRl = &carserver.TirePressureState_TpmsSoftWarningRl{TpmsSoftWarningRl: true}
		},
		"tpms_soft_warning_rr": func(s *carserver.TirePressureState) {
			s.OptionalTpmsSoftWarningRr = &carserver.TirePressureState_TpmsSoftWarningRr{TpmsSoftWarningRr: true}
		},
	}
	for key, set := range setters {
		t.Run(key, func(t *testing.T) {
			s := &carserver.TirePressureState{}
			set(s)
			var got map[string]any
			if err := json.Unmarshal([]byte(tirePressureJSON(t, s)), &got); err != nil {
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
			if bools != 8 {
				t.Errorf("%d boolean keys, want 8", bools)
			}
		})
	}

	got := TirePressureFromBle(&carserver.VehicleData{TirePressureState: &carserver.TirePressureState{
		OptionalTpmsPressureFl:     &carserver.TirePressureState_TpmsPressureFl{TpmsPressureFl: 1},
		OptionalTpmsPressureFr:     &carserver.TirePressureState_TpmsPressureFr{TpmsPressureFr: 2},
		OptionalTpmsPressureRl:     &carserver.TirePressureState_TpmsPressureRl{TpmsPressureRl: 3},
		OptionalTpmsPressureRr:     &carserver.TirePressureState_TpmsPressureRr{TpmsPressureRr: 4},
		TpmsLastSeenPressureTimeFl: &timestamppb.Timestamp{Seconds: 11},
		TpmsLastSeenPressureTimeFr: &timestamppb.Timestamp{Seconds: 12},
		TpmsLastSeenPressureTimeRl: &timestamppb.Timestamp{Seconds: 13},
		TpmsLastSeenPressureTimeRr: &timestamppb.Timestamp{Seconds: 14},
		OptionalTpmsRcpFrontValue:  &carserver.TirePressureState_TpmsRcpFrontValue{TpmsRcpFrontValue: 5},
		OptionalTpmsRcpRearValue:   &carserver.TirePressureState_TpmsRcpRearValue{TpmsRcpRearValue: 6},
	}})
	want := TirePressure{
		TpmsPressureFl: 1, TpmsPressureFr: 2, TpmsPressureRl: 3, TpmsPressureRr: 4,
		TpmsLastSeenPressureTimeFl: 11, TpmsLastSeenPressureTimeFr: 12, TpmsLastSeenPressureTimeRl: 13, TpmsLastSeenPressureTimeRr: 14,
		TpmsRcpFrontValue: 5, TpmsRcpRearValue: 6,
	}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// Explicit zero and false are on the wire: the keys are served, with those values.
func TestTirePressureExplicitZero(t *testing.T) {
	s := &carserver.TirePressureState{
		OptionalTpmsPressureFl:    &carserver.TirePressureState_TpmsPressureFl{},
		OptionalTpmsHardWarningFl: &carserver.TirePressureState_TpmsHardWarningFl{},
		OptionalTpmsRcpRearValue:  &carserver.TirePressureState_TpmsRcpRearValue{},
	}
	if got := tirePressureJSON(t, s); got != tirePressureZero {
		t.Errorf("got  %s\nwant %s", got, tirePressureZero)
	}
}

// D-1017-03: NaN and +-Inf are served as 0, the other fields are untouched.
func TestTirePressureNonFinite(t *testing.T) {
	for name, v := range map[string]float32{
		"NaN":  float32(math.NaN()),
		"+Inf": float32(math.Inf(1)),
		"-Inf": float32(math.Inf(-1)),
	} {
		t.Run(name, func(t *testing.T) {
			s := &carserver.TirePressureState{
				OptionalTpmsPressureFl:    &carserver.TirePressureState_TpmsPressureFl{TpmsPressureFl: v},
				OptionalTpmsPressureFr:    &carserver.TirePressureState_TpmsPressureFr{TpmsPressureFr: 2.5},
				OptionalTpmsRcpFrontValue: &carserver.TirePressureState_TpmsRcpFrontValue{TpmsRcpFrontValue: v},
				OptionalTpmsRcpRearValue:  &carserver.TirePressureState_TpmsRcpRearValue{TpmsRcpRearValue: 3},
			}
			got := TirePressureFromBle(&carserver.VehicleData{TirePressureState: s})
			if got.TpmsPressureFl != 0 || got.TpmsRcpFrontValue != 0 || got.TpmsPressureFr != 2.5 || got.TpmsRcpRearValue != 3 {
				t.Errorf("got %+v", got)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Errorf("json.Marshal: %v", err)
			}
		})
	}
}

func TestTirePressureZeroValues(t *testing.T) {
	for name, got := range map[string]string{
		"nil VehicleData":     mustJSON(t, TirePressureFromBle(nil)),
		"empty VehicleData":   mustJSON(t, TirePressureFromBle(&carserver.VehicleData{})),
		"empty state message": tirePressureJSON(t, &carserver.TirePressureState{}),
	} {
		if got != tirePressureZero {
			t.Errorf("%s: got %s\nwant %s", name, got, tirePressureZero)
		}
	}
	typ := reflect.TypeOf(TirePressure{})
	if typ.NumField() != 19 {
		t.Errorf("%d fields, want 19", typ.NumField())
	}
	for i := 0; i < typ.NumField(); i++ {
		if tag := typ.Field(i).Tag.Get("json"); tag == "" || strings.Contains(tag, "omitempty") {
			t.Errorf("field %s has json tag %q", typ.Field(i).Name, tag)
		}
	}
}

// A new field in the SDK message must be exposed: fail instead of dropping it silently. Oneof
// members are fields of the message descriptor too, under their own name.
func TestTirePressureCoversProtoFields(t *testing.T) {
	var keys map[string]any
	if err := json.Unmarshal([]byte(tirePressureZero), &keys); err != nil {
		t.Fatal(err)
	}
	fields := (&carserver.TirePressureState{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		if name := string(fields.Get(i).Name()); keys[name] == nil {
			t.Errorf("proto field %q of TirePressureState is not a key of tire_pressure", name)
		}
	}
	if fields.Len() != len(keys) {
		t.Errorf("proto has %d fields, model has %d keys", fields.Len(), len(keys))
	}
}
