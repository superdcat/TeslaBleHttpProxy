package models

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const preconditionScheduleEntryZero = `{"id":0,"name":"","days_of_week":0,"precondition_time":0,"one_time":false,"enabled":false,"latitude":0,"longitude":0}`

const preconditioningScheduleDataZero = `{"timestamp":0,"precondition_schedules":[],"preconditioning_schedule_window":` + preconditionScheduleEntryZero +
	`,"max_num_precondition_schedules":0,"next_schedule":false}`

func preconditioningScheduleDataJSON(t *testing.T, s *carserver.PreconditioningScheduleState) string {
	t.Helper()
	return mustJSON(t, PreconditioningScheduleDataFromBle(&carserver.VehicleData{PreconditioningScheduleState: s}))
}

func preconditioningStateWith(schedules []*carserver.PreconditionSchedule, window *carserver.PreconditionSchedule) *carserver.PreconditioningScheduleState {
	return &carserver.PreconditioningScheduleState{
		PreconditionSchedules:                 schedules,
		OptionalPreconditioningScheduleWindow: &carserver.PreconditioningScheduleState_PreconditioningScheduleWindow{PreconditioningScheduleWindow: window},
	}
}

// AC1: a hand-built message is converted with id, days, time and switches exact, in order.
func TestPreconditioningScheduleDataJSON(t *testing.T) {
	s := &carserver.PreconditioningScheduleState{
		PreconditionSchedules: []*carserver.PreconditionSchedule{
			{Id: 1767225700, Name: "Home", DaysOfWeek: 62, PreconditionTime: 450, Enabled: true, Latitude: 45.764, Longitude: 4.8357},
			{Id: 1767225701, DaysOfWeek: 1, PreconditionTime: 600, OneTime: true, Latitude: 44.8378, Longitude: -0.5792},
		},
		OptionalPreconditioningScheduleWindow: &carserver.PreconditioningScheduleState_PreconditioningScheduleWindow{PreconditioningScheduleWindow: &carserver.PreconditionSchedule{Id: 1767225702, DaysOfWeek: 2, PreconditionTime: 480, Enabled: true}},
		OptionalMaxNumPreconditionSchedules:   &carserver.PreconditioningScheduleState_MaxNumPreconditionSchedules{MaxNumPreconditionSchedules: 10},
		OptionalNextSchedule:                  &carserver.PreconditioningScheduleState_NextSchedule{NextSchedule: true},
		Timestamp:                             &timestamppb.Timestamp{Seconds: 1767254400},
	}
	want := `{"timestamp":1767254400,"precondition_schedules":[` +
		`{"id":1767225700,"name":"Home","days_of_week":62,"precondition_time":450,"one_time":false,"enabled":true,"latitude":45.764,"longitude":4.8357},` +
		`{"id":1767225701,"name":"","days_of_week":1,"precondition_time":600,"one_time":true,"enabled":false,"latitude":44.8378,"longitude":-0.5792}],` +
		`"preconditioning_schedule_window":{"id":1767225702,"name":"","days_of_week":2,"precondition_time":480,"one_time":false,"enabled":true,"latitude":0,"longitude":0},` +
		`"max_num_precondition_schedules":10,"next_schedule":true}`
	if got := preconditioningScheduleDataJSON(t, s); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// AC2: an empty list is [], never null.
func TestPreconditioningScheduleDataEmptyList(t *testing.T) {
	s := &carserver.PreconditioningScheduleState{
		OptionalMaxNumPreconditionSchedules: &carserver.PreconditioningScheduleState_MaxNumPreconditionSchedules{MaxNumPreconditionSchedules: 10},
	}
	got := preconditioningScheduleDataJSON(t, s)
	if !strings.Contains(got, `"precondition_schedules":[]`) || strings.Contains(got, "null") || !strings.Contains(got, `"max_num_precondition_schedules":10`) {
		t.Errorf("got %s", got)
	}
}

func TestPreconditionScheduleEntryFieldMapping(t *testing.T) {
	// One boolean at a time: a swapped field is caught.
	for _, name := range []string{"one_time", "enabled"} {
		e := &carserver.PreconditionSchedule{OneTime: name == "one_time", Enabled: name == "enabled"}
		var keys map[string]any
		if err := json.Unmarshal([]byte(mustJSON(t, preconditionScheduleEntry(e))), &keys); err != nil {
			t.Fatal(err)
		}
		for k, v := range keys {
			if b, ok := v.(bool); ok && b != (k == name) {
				t.Errorf("%s set alone: key %s = %v", name, k, b)
			}
		}
	}

	got := preconditionScheduleEntry(&carserver.PreconditionSchedule{Id: 1<<53 + 1, Name: "n", DaysOfWeek: 127, PreconditionTime: 1439})
	if got.ID != 1<<53+1 || got.Name != "n" || got.DaysOfWeek != 127 || got.PreconditionTime != 1439 {
		t.Errorf("got %+v", got)
	}
	if j := mustJSON(t, preconditionScheduleEntry(&carserver.PreconditionSchedule{Id: 1<<53 + 1})); !strings.Contains(j, `"id":9007199254740993,`) {
		t.Errorf("id above 2^53 not exact: %s", j)
	}
	if j := mustJSON(t, preconditionScheduleEntry(&carserver.PreconditionSchedule{Id: math.MaxUint64})); !strings.Contains(j, `"id":18446744073709551615,`) {
		t.Errorf("max id not exact: %s", j)
	}

	// List and window kept apart.
	s := preconditioningStateWith([]*carserver.PreconditionSchedule{{Id: 1}, {Id: 2}}, &carserver.PreconditionSchedule{Id: 3})
	d := PreconditioningScheduleDataFromBle(&carserver.VehicleData{PreconditioningScheduleState: s})
	if len(d.PreconditionSchedules) != 2 || d.PreconditionSchedules[0].ID != 1 || d.PreconditionSchedules[1].ID != 2 || d.PreconditioningScheduleWindow.ID != 3 {
		t.Errorf("got %+v", d)
	}
}

// NaN and +-Inf coordinates are served as 0; json.Marshal must not fail.
func TestPreconditioningScheduleDataNonFinite(t *testing.T) {
	for name, v := range map[string]float32{
		"NaN":  float32(math.NaN()),
		"+Inf": float32(math.Inf(1)),
		"-Inf": float32(math.Inf(-1)),
	} {
		t.Run(name, func(t *testing.T) {
			s := preconditioningStateWith(
				[]*carserver.PreconditionSchedule{{Id: 7, Latitude: v, Longitude: v}},
				&carserver.PreconditionSchedule{Id: 8, Latitude: v, Longitude: v})
			got := PreconditioningScheduleDataFromBle(&carserver.VehicleData{PreconditioningScheduleState: s})
			for _, e := range []PreconditionScheduleEntry{got.PreconditionSchedules[0], got.PreconditioningScheduleWindow} {
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

func TestPreconditioningScheduleDataZeroValues(t *testing.T) {
	for name, got := range map[string]string{
		"nil VehicleData":     mustJSON(t, PreconditioningScheduleDataFromBle(nil)),
		"empty VehicleData":   mustJSON(t, PreconditioningScheduleDataFromBle(&carserver.VehicleData{})),
		"empty state message": preconditioningScheduleDataJSON(t, &carserver.PreconditioningScheduleState{}),
	} {
		if got != preconditioningScheduleDataZero {
			t.Errorf("%s: got %s\nwant %s", name, got, preconditioningScheduleDataZero)
		}
	}
	if got := preconditionScheduleEntry(nil); got != (PreconditionScheduleEntry{}) {
		t.Errorf("nil entry: got %+v", got)
	}
	// A nil element of the list gives a zero entry, without panic.
	s := &carserver.PreconditioningScheduleState{PreconditionSchedules: []*carserver.PreconditionSchedule{nil, {Id: 5}}}
	d := PreconditioningScheduleDataFromBle(&carserver.VehicleData{PreconditioningScheduleState: s})
	if len(d.PreconditionSchedules) != 2 || d.PreconditionSchedules[0] != (PreconditionScheduleEntry{}) || d.PreconditionSchedules[1].ID != 5 {
		t.Errorf("got %+v", d.PreconditionSchedules)
	}
	checkAlwaysPresentFields(t, reflect.TypeOf(PreconditioningScheduleData{}), 5)
	checkAlwaysPresentFields(t, reflect.TypeOf(PreconditionScheduleEntry{}), 8)
}

// A new field in the SDK message must be exposed: the keys equal the proto fields, no exclusion list.
func TestPreconditioningScheduleDataCoversProtoFields(t *testing.T) {
	top := jsonKeys(t, preconditioningScheduleDataZero)
	if want := protoFieldNames(&carserver.PreconditioningScheduleState{}); !slices.Equal(top, want) {
		t.Errorf("preconditioning_schedule_data keys %v, proto fields %v", top, want)
	}
	entry := jsonKeys(t, preconditionScheduleEntryZero)
	if want := protoFieldNames(&carserver.PreconditionSchedule{}); !slices.Equal(entry, want) {
		t.Errorf("schedule keys %v, proto fields %v", entry, want)
	}
}
