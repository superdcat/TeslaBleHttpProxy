package models

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const chargeScheduleEntryZero = `{"id":0,"name":"","days_of_week":0,"start_enabled":false,"start_time":0,"end_enabled":false,"end_time":0,"one_time":false,"enabled":false,"latitude":0,"longitude":0}`

const chargeScheduleDataZero = `{"timestamp":0,"charge_schedules":[],"charge_schedule_window":` + chargeScheduleEntryZero +
	`,"charge_buffer":0,"max_num_charge_schedules":0,"next_schedule":false,"show_schedule_complete_state":false}`

func chargeScheduleDataJSON(t *testing.T, s *carserver.ChargeScheduleState) string {
	t.Helper()
	return mustJSON(t, ChargeScheduleDataFromBle(&carserver.VehicleData{ChargeScheduleState: s}))
}

func chargeStateWith(schedules []*carserver.ChargeSchedule, window *carserver.ChargeSchedule) *carserver.ChargeScheduleState {
	return &carserver.ChargeScheduleState{
		ChargeSchedules:              schedules,
		OptionalChargeScheduleWindow: &carserver.ChargeScheduleState_ChargeScheduleWindow{ChargeScheduleWindow: window},
	}
}

// protoFieldNames returns the sorted field names of a proto message.
func protoFieldNames(m proto.Message) []string {
	fields := m.ProtoReflect().Descriptor().Fields()
	names := make([]string, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		names = append(names, string(fields.Get(i).Name()))
	}
	slices.Sort(names)
	return names
}

// jsonKeys returns the sorted top-level keys of a JSON object.
func jsonKeys(t *testing.T, raw string) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// checkAlwaysPresentFields checks the field count and that no field is omitempty.
func checkAlwaysPresentFields(t *testing.T, typ reflect.Type, n int) {
	t.Helper()
	if typ.NumField() != n {
		t.Errorf("%s has %d fields, want %d", typ.Name(), typ.NumField(), n)
	}
	for i := 0; i < typ.NumField(); i++ {
		if tag := typ.Field(i).Tag.Get("json"); tag == "" || strings.Contains(tag, "omitempty") {
			t.Errorf("%s.%s has json tag %q", typ.Name(), typ.Field(i).Name, tag)
		}
	}
}

// AC1: a hand-built message is converted with id, days, times and switches exact, in order.
func TestChargeScheduleDataJSON(t *testing.T) {
	s := &carserver.ChargeScheduleState{
		ChargeSchedules: []*carserver.ChargeSchedule{
			{Id: 1767225600, Name: "Home", DaysOfWeek: 62, StartEnabled: true, StartTime: 1380, EndEnabled: true, EndTime: 420, Enabled: true, Latitude: 45.764, Longitude: 4.8357},
			{Id: 1767225601, Name: "Work", DaysOfWeek: 65, StartEnabled: true, StartTime: 600, EndTime: 900, OneTime: true, Latitude: 44.8378, Longitude: -0.5792},
		},
		OptionalChargeScheduleWindow:      &carserver.ChargeScheduleState_ChargeScheduleWindow{ChargeScheduleWindow: &carserver.ChargeSchedule{Id: 1767225602, DaysOfWeek: 2, StartEnabled: true, StartTime: 1320, EndEnabled: true, EndTime: 360, Enabled: true}},
		OptionalChargeBuffer:              &carserver.ChargeScheduleState_ChargeBuffer{ChargeBuffer: 15},
		OptionalMaxNumChargeSchedules:     &carserver.ChargeScheduleState_MaxNumChargeSchedules{MaxNumChargeSchedules: 10},
		OptionalNextSchedule:              &carserver.ChargeScheduleState_NextSchedule{NextSchedule: true},
		OptionalShowScheduleCompleteState: &carserver.ChargeScheduleState_ShowScheduleCompleteState{},
		Timestamp:                         &timestamppb.Timestamp{Seconds: 1767254400},
	}
	want := `{"timestamp":1767254400,"charge_schedules":[` +
		`{"id":1767225600,"name":"Home","days_of_week":62,"start_enabled":true,"start_time":1380,"end_enabled":true,"end_time":420,"one_time":false,"enabled":true,"latitude":45.764,"longitude":4.8357},` +
		`{"id":1767225601,"name":"Work","days_of_week":65,"start_enabled":true,"start_time":600,"end_enabled":false,"end_time":900,"one_time":true,"enabled":false,"latitude":44.8378,"longitude":-0.5792}],` +
		`"charge_schedule_window":{"id":1767225602,"name":"","days_of_week":2,"start_enabled":true,"start_time":1320,"end_enabled":true,"end_time":360,"one_time":false,"enabled":true,"latitude":0,"longitude":0},` +
		`"charge_buffer":15,"max_num_charge_schedules":10,"next_schedule":true,"show_schedule_complete_state":false}`
	if got := chargeScheduleDataJSON(t, s); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// AC2: an empty list is [], never null.
func TestChargeScheduleDataEmptyList(t *testing.T) {
	s := &carserver.ChargeScheduleState{
		OptionalMaxNumChargeSchedules: &carserver.ChargeScheduleState_MaxNumChargeSchedules{MaxNumChargeSchedules: 10},
	}
	got := chargeScheduleDataJSON(t, s)
	if !strings.Contains(got, `"charge_schedules":[]`) || strings.Contains(got, "null") || !strings.Contains(got, `"max_num_charge_schedules":10`) {
		t.Errorf("got %s", got)
	}
}

func TestChargeScheduleEntryFieldMapping(t *testing.T) {
	// One boolean at a time: a swapped field is caught.
	for _, name := range []string{"start_enabled", "end_enabled", "one_time", "enabled"} {
		e := &carserver.ChargeSchedule{}
		switch name {
		case "start_enabled":
			e.StartEnabled = true
		case "end_enabled":
			e.EndEnabled = true
		case "one_time":
			e.OneTime = true
		case "enabled":
			e.Enabled = true
		}
		var keys map[string]any
		if err := json.Unmarshal([]byte(mustJSON(t, chargeScheduleEntry(e))), &keys); err != nil {
			t.Fatal(err)
		}
		for k, v := range keys {
			if b, ok := v.(bool); ok && b != (k == name) {
				t.Errorf("%s set alone: key %s = %v", name, k, b)
			}
		}
	}

	got := chargeScheduleEntry(&carserver.ChargeSchedule{Id: 1<<53 + 1, Name: "n", DaysOfWeek: 127, StartTime: 1, EndTime: 2})
	if got.ID != 1<<53+1 || got.Name != "n" || got.DaysOfWeek != 127 || got.StartTime != 1 || got.EndTime != 2 {
		t.Errorf("got %+v", got)
	}
	if j := mustJSON(t, chargeScheduleEntry(&carserver.ChargeSchedule{Id: 1<<53 + 1})); !strings.Contains(j, `"id":9007199254740993,`) {
		t.Errorf("id above 2^53 not exact: %s", j)
	}
	if j := mustJSON(t, chargeScheduleEntry(&carserver.ChargeSchedule{Id: math.MaxUint64})); !strings.Contains(j, `"id":18446744073709551615,`) {
		t.Errorf("max id not exact: %s", j)
	}

	// Signed buffer, and list and window kept apart.
	s := chargeStateWith([]*carserver.ChargeSchedule{{Id: 1}, {Id: 2}}, &carserver.ChargeSchedule{Id: 3})
	s.OptionalChargeBuffer = &carserver.ChargeScheduleState_ChargeBuffer{ChargeBuffer: -5}
	d := ChargeScheduleDataFromBle(&carserver.VehicleData{ChargeScheduleState: s})
	if d.ChargeBuffer != -5 || len(d.ChargeSchedules) != 2 || d.ChargeSchedules[0].ID != 1 || d.ChargeSchedules[1].ID != 2 || d.ChargeScheduleWindow.ID != 3 {
		t.Errorf("got %+v", d)
	}
}

// NaN and +-Inf coordinates are served as 0; json.Marshal must not fail.
func TestChargeScheduleDataNonFinite(t *testing.T) {
	for name, v := range map[string]float32{
		"NaN":  float32(math.NaN()),
		"+Inf": float32(math.Inf(1)),
		"-Inf": float32(math.Inf(-1)),
	} {
		t.Run(name, func(t *testing.T) {
			s := chargeStateWith(
				[]*carserver.ChargeSchedule{{Id: 7, Latitude: v, Longitude: v}},
				&carserver.ChargeSchedule{Id: 8, Latitude: v, Longitude: v})
			got := ChargeScheduleDataFromBle(&carserver.VehicleData{ChargeScheduleState: s})
			for _, e := range []ChargeScheduleEntry{got.ChargeSchedules[0], got.ChargeScheduleWindow} {
				if e.Latitude != 0 || e.Longitude != 0 || e.ID == 0 {
					t.Errorf("got %+v", e)
				}
			}
			if _, err := json.Marshal(got); err != nil {
				t.Errorf("json.Marshal: %v", err)
			}
		})
	}
}

func TestChargeScheduleDataZeroValues(t *testing.T) {
	for name, got := range map[string]string{
		"nil VehicleData":     mustJSON(t, ChargeScheduleDataFromBle(nil)),
		"empty VehicleData":   mustJSON(t, ChargeScheduleDataFromBle(&carserver.VehicleData{})),
		"empty state message": chargeScheduleDataJSON(t, &carserver.ChargeScheduleState{}),
	} {
		if got != chargeScheduleDataZero {
			t.Errorf("%s: got %s\nwant %s", name, got, chargeScheduleDataZero)
		}
	}
	if got := chargeScheduleEntry(nil); got != (ChargeScheduleEntry{}) {
		t.Errorf("nil entry: got %+v", got)
	}
	// A nil element of the list gives a zero entry, without panic.
	s := &carserver.ChargeScheduleState{ChargeSchedules: []*carserver.ChargeSchedule{nil, {Id: 5}}}
	d := ChargeScheduleDataFromBle(&carserver.VehicleData{ChargeScheduleState: s})
	if len(d.ChargeSchedules) != 2 || d.ChargeSchedules[0] != (ChargeScheduleEntry{}) || d.ChargeSchedules[1].ID != 5 {
		t.Errorf("got %+v", d.ChargeSchedules)
	}
	checkAlwaysPresentFields(t, reflect.TypeOf(ChargeScheduleData{}), 7)
	checkAlwaysPresentFields(t, reflect.TypeOf(ChargeScheduleEntry{}), 11)
}

// A new field in the SDK message must be exposed: the keys equal the proto fields, no exclusion list.
func TestChargeScheduleDataCoversProtoFields(t *testing.T) {
	top := jsonKeys(t, chargeScheduleDataZero)
	if want := protoFieldNames(&carserver.ChargeScheduleState{}); !slices.Equal(top, want) {
		t.Errorf("charge_schedule_data keys %v, proto fields %v", top, want)
	}
	entry := jsonKeys(t, chargeScheduleEntryZero)
	if want := protoFieldNames(&carserver.ChargeSchedule{}); !slices.Equal(entry, want) {
		t.Errorf("schedule keys %v, proto fields %v", entry, want)
	}
}
