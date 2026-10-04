package commands

import (
	"context"
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// frozenScheduleUnix is 2026-01-01T00:00:00Z: the id generated while the clock is frozen.
const frozenScheduleUnix = 1767225600

// bodyCase is one row of TestCommandBodies.
type bodyCase struct {
	command string
	body    string // JSON object; "" = no body
	call    string // SDK call expected for a valid body
	reason  string // error expected for an invalid body
}

// freezeScheduleIDs replaces the id generator by one whose clock stays at unix, and restores it.
func freezeScheduleIDs(t *testing.T, unix int64) {
	t.Helper()
	previous := scheduleIDs
	scheduleIDs = &scheduleIDSource{now: func() time.Time { return time.Unix(unix, 0) }}
	t.Cleanup(func() { scheduleIDs = previous })
}

// scheduleCall builds the AddChargeSchedule call expected for the usual cases.
func scheduleCall(id, days, start, end string) string {
	return "AddChargeSchedule(id=" + id + ",days=" + days + "," + start + "," + end +
		",one_time=false,enabled=true,lat=1,lon=2,name=\"\")"
}

// scheduleBodyCases are the charge schedule rows of TestCommandBodies (UC1014). The generated id is
// 1767225600 (frozen clock). The first three bodies are those of wimaha PR #153 (woodydaocas).
var scheduleBodyCases = []bodyCase{
	// add_charge_schedule, valid.
	{"add_charge_schedule", `{"id":3,"days_of_week":"Weekdays","start_enabled":true,"start_time":480,"end_enabled":true,"end_time":1020,"one_time":false,"enabled":true,"lat":48.8566,"lon":2.3522}`,
		"AddChargeSchedule(id=3,days=0111110,start=true/480,end=true/1020,one_time=false,enabled=true,lat=48.8566,lon=2.3522,name=\"\")", ""},
	{"add_charge_schedule", `{"days_of_week":"All","start_enabled":true,"start_time":60,"end_enabled":false,"enabled":true,"lat":0,"lon":0}`,
		"AddChargeSchedule(id=1767225600,days=1111111,start=true/60,end=false/0,one_time=false,enabled=true,lat=0,lon=0,name=\"\")", ""},
	{"add_charge_schedule", `{"days_of_week":"mon,wed,fri","start_enabled":false,"end_enabled":true,"end_time":300,"one_time":true,"enabled":false,"lat":-33.86,"lon":151.2}`,
		"AddChargeSchedule(id=1767225600,days=0101010,start=false/0,end=true/300,one_time=true,enabled=false,lat=-33.86,lon=151.2,name=\"\")", ""},
	// id kept when present (number or decimal string), generated when absent, null or 0.
	{"add_charge_schedule", `{"id":"1767225600","days_of_week":"all","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1767225600", "1111111", "start=true/1", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":0,"days_of_week":"all","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1767225600", "1111111", "start=true/1", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":null,"days_of_week":"all","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1767225600", "1111111", "start=true/1", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":"9007199254740993","days_of_week":"all","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("9007199254740993", "1111111", "start=true/1", "end=false/0"), ""},
	// days_of_week forms.
	{"add_charge_schedule", `{"id":1,"days_of_week":" Sun , SATURDAY ","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "1000001", "start=true/1", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":"tues,thurs","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "0010100", "start=true/1", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":"tue,thu","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "0010100", "start=true/1", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":"sun,all","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "1111111", "start=true/1", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":34,"start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "0100010", "start=true/1", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":"34","start_time":1,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "0100010", "start=true/1", "end=false/0"), ""},
	// Times: bounds, end before start (past midnight), a switch off keeps its time, name ignored, strings.
	{"add_charge_schedule", `{"id":1,"days_of_week":"all","start_time":0,"end_time":1439,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "1111111", "start=true/0", "end=true/1439"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":"all","start_time":1380,"end_time":360,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "1111111", "start=true/1380", "end=true/360"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":"all","start_enabled":true,"start_time":60,"end_enabled":false,"end_time":120,"enabled":true,"lat":1,"lon":2}`,
		scheduleCall("1", "1111111", "start=true/60", "end=false/120"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":"all","start_time":60,"enabled":true,"lat":1,"lon":2,"name":"Home"}`,
		scheduleCall("1", "1111111", "start=true/60", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":"1","days_of_week":"all","start_enabled":"true","start_time":"60","enabled":"true","one_time":"false","lat":"1","lon":"2"}`,
		scheduleCall("1", "1111111", "start=true/60", "end=false/0"), ""},
	{"add_charge_schedule", `{"id":1,"days_of_week":"all","start_time":60,"enabled":true,"lat":90,"lon":-180}`,
		"AddChargeSchedule(id=1,days=1111111,start=true/60,end=false/0,one_time=false,enabled=true,lat=90,lon=-180,name=\"\")", ""},

	// add_charge_schedule, refused (the first faulty field wins).
	{"add_charge_schedule", ``, "", "invalid request body: days_of_week missing"},
	{"add_charge_schedule", `{"start_time":"abc"}`, "", "invalid request body: days_of_week missing"},
	{"add_charge_schedule", `{"id":-1,"days_of_week":"x"}`, "", "invalid request body: id must be a non-negative integer"},
	{"add_charge_schedule", `{"id":1.5,"days_of_week":"all"}`, "", "invalid request body: id must be a non-negative integer"},
	{"add_charge_schedule", `{"id":9007199254740992,"days_of_week":"all"}`, "", "invalid request body: id is out of range"},
	{"add_charge_schedule", `{"id":"18446744073709551616","days_of_week":"all"}`, "", "invalid request body: id is out of range"},
	{"add_charge_schedule", `{"id":"abc","days_of_week":"all"}`, "", "invalid request body: id is not a valid integer"},
	{"add_charge_schedule", `{"id":"-1","days_of_week":"all"}`, "", "invalid request body: id is not a valid integer"},
	{"add_charge_schedule", `{"id":true,"days_of_week":"all"}`, "", "invalid request body: id must be a number or a numeric string"},
	{"add_charge_schedule", `{"id":[1],"days_of_week":"all"}`, "", "invalid request body: id must be a number or a numeric string"},
	{"add_charge_schedule", `{"days_of_week":null}`, "", "invalid request body: days_of_week missing"},
	{"add_charge_schedule", `{"days_of_week":"x","start_time":"abc"}`, "", "invalid request body: days_of_week contains an unknown day name"},
	{"add_charge_schedule", `{"days_of_week":""}`, "", "invalid request body: days_of_week contains an unknown day name"},
	{"add_charge_schedule", `{"days_of_week":"funday"}`, "", "invalid request body: days_of_week contains an unknown day name"},
	{"add_charge_schedule", `{"days_of_week":"mon,,fri"}`, "", "invalid request body: days_of_week contains an unknown day name"},
	{"add_charge_schedule", `{"days_of_week":"mon,"}`, "", "invalid request body: days_of_week contains an unknown day name"},
	{"add_charge_schedule", `{"days_of_week":"mon fri"}`, "", "invalid request body: days_of_week contains an unknown day name"},
	{"add_charge_schedule", `{"days_of_week":"ſun"}`, "", "invalid request body: days_of_week contains an unknown day name"},
	{"add_charge_schedule", `{"days_of_week":0}`, "", "invalid request body: days_of_week must be a bitmask between 1 and 127"},
	{"add_charge_schedule", `{"days_of_week":128}`, "", "invalid request body: days_of_week must be a bitmask between 1 and 127"},
	{"add_charge_schedule", `{"days_of_week":-1}`, "", "invalid request body: days_of_week must be a bitmask between 1 and 127"},
	{"add_charge_schedule", `{"days_of_week":1.5}`, "", "invalid request body: days_of_week must be a bitmask between 1 and 127"},
	{"add_charge_schedule", `{"days_of_week":"0"}`, "", "invalid request body: days_of_week must be a bitmask between 1 and 127"},
	{"add_charge_schedule", `{"days_of_week":"128"}`, "", "invalid request body: days_of_week must be a bitmask between 1 and 127"},
	{"add_charge_schedule", `{"days_of_week":true}`, "", "invalid request body: days_of_week must be a string or a number"},
	{"add_charge_schedule", `{"days_of_week":["mon"]}`, "", "invalid request body: days_of_week must be a string or a number"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":1440}`, "", "invalid request body: start_time must be an integer between 0 and 1439"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":-1}`, "", "invalid request body: start_time must be an integer between 0 and 1439"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":600.5}`, "", "invalid request body: start_time must be an integer between 0 and 1439"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":"abc"}`, "", "invalid request body: start_time is not a valid integer"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":"08:00"}`, "", "invalid request body: start_time is not a valid integer"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":" 600"}`, "", "invalid request body: start_time is not a valid integer"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":true}`, "", "invalid request body: start_time must be a number or a numeric string"},
	{"add_charge_schedule", `{"days_of_week":"all","start_enabled":true}`, "", "invalid request body: start_time missing"},
	{"add_charge_schedule", `{"days_of_week":"all","start_enabled":false,"end_enabled":true}`, "", "invalid request body: end_time missing"},
	{"add_charge_schedule", `{"days_of_week":"all","start_enabled":"yes"}`, "", "invalid request body: start_enabled is not a valid boolean"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"end_time":1440}`, "", "invalid request body: end_time must be an integer between 0 and 1439"},
	{"add_charge_schedule", `{"days_of_week":"all","start_enabled":false,"end_enabled":false,"start_time":1,"end_time":2,"enabled":true,"lat":1,"lon":2}`, "", "invalid request body: start_enabled or end_enabled must be true"},
	{"add_charge_schedule", `{"days_of_week":"all","enabled":true,"lat":1,"lon":2}`, "", "invalid request body: start_enabled or end_enabled must be true"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"one_time":"maybe"}`, "", "invalid request body: one_time is not a valid boolean"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"lat":1,"lon":2}`, "", "invalid request body: enabled missing"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":"yes","lat":1,"lon":2}`, "", "invalid request body: enabled is not a valid boolean"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":1,"lat":1,"lon":2}`, "", `invalid request body: enabled must be a boolean or "true"/"false"`},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":true,"lon":2}`, "", "invalid request body: lat missing"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":true,"latitude":1,"longitude":2}`, "", "invalid request body: lat missing"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":true,"lat":1}`, "", "invalid request body: lon missing"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":true,"lat":91,"lon":2}`, "", "invalid request body: lat must be between -90 and 90 degrees"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":true,"lat":1,"lon":-181}`, "", "invalid request body: lon must be between -180 and 180 degrees"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":true,"lat":"x","lon":2}`, "", "invalid request body: lat is not a valid number"},
	{"add_charge_schedule", `{"days_of_week":"all","start_time":60,"enabled":true,"lat":"NaN","lon":2}`, "", "invalid request body: lat is not a valid number"},

	// remove_charge_schedule: id >= 1, number or decimal string.
	{"remove_charge_schedule", `{"id":7}`, "RemoveChargeSchedule(7)", ""},
	{"remove_charge_schedule", `{"id":"7"}`, "RemoveChargeSchedule(7)", ""},
	{"remove_charge_schedule", `{"id":"18446744073709551615"}`, "RemoveChargeSchedule(18446744073709551615)", ""},
	{"remove_charge_schedule", ``, "", "invalid request body: id missing"},
	{"remove_charge_schedule", `{}`, "", "invalid request body: id missing"},
	{"remove_charge_schedule", `{"id":null}`, "", "invalid request body: id missing"},
	{"remove_charge_schedule", `{"id":0}`, "", "invalid request body: id must be a positive integer"},
	{"remove_charge_schedule", `{"id":"0"}`, "", "invalid request body: id must be a positive integer"},
	{"remove_charge_schedule", `{"id":-1}`, "", "invalid request body: id must be a positive integer"},
	{"remove_charge_schedule", `{"id":1.5}`, "", "invalid request body: id must be a positive integer"},
	{"remove_charge_schedule", `{"id":9007199254740992}`, "", "invalid request body: id is out of range"},
	{"remove_charge_schedule", `{"id":"abc"}`, "", "invalid request body: id is not a valid integer"},
	{"remove_charge_schedule", `{"id":"+7"}`, "", "invalid request body: id is not a valid integer"},
	{"remove_charge_schedule", `{"id":true}`, "", "invalid request body: id must be a number or a numeric string"},

	// set_scheduled_charging: time required only when enabling.
	{"set_scheduled_charging", `{"enable":true,"time":120}`, "ScheduleCharging(true,2h0m0s)", ""},
	{"set_scheduled_charging", `{"enable":"true","time":"1439"}`, "ScheduleCharging(true,23h59m0s)", ""},
	{"set_scheduled_charging", `{"enable":true,"time":0}`, "ScheduleCharging(true,0s)", ""},
	{"set_scheduled_charging", `{"enable":false}`, "ScheduleCharging(false,0s)", ""},
	{"set_scheduled_charging", `{"enable":false,"time":60}`, "ScheduleCharging(false,1h0m0s)", ""},
	{"set_scheduled_charging", ``, "", "invalid request body: enable missing"},
	{"set_scheduled_charging", `{"on":true,"time":60}`, "", "invalid request body: enable missing"},
	{"set_scheduled_charging", `{"enable":"yes","time":60}`, "", "invalid request body: enable is not a valid boolean"},
	{"set_scheduled_charging", `{"enable":1,"time":60}`, "", `invalid request body: enable must be a boolean or "true"/"false"`},
	{"set_scheduled_charging", `{"enable":true}`, "", "invalid request body: time missing"},
	{"set_scheduled_charging", `{"enable":true,"time":null}`, "", "invalid request body: time missing"},
	{"set_scheduled_charging", `{"enable":true,"time":1440}`, "", "invalid request body: time must be an integer between 0 and 1439"},
	{"set_scheduled_charging", `{"enable":false,"time":-1}`, "", "invalid request body: time must be an integer between 0 and 1439"},
	{"set_scheduled_charging", `{"enable":true,"time":600.5}`, "", "invalid request body: time must be an integer between 0 and 1439"},
	{"set_scheduled_charging", `{"enable":true,"time":"abc"}`, "", "invalid request body: time is not a valid integer"},
	{"set_scheduled_charging", `{"enable":true,"time":true}`, "", "invalid request body: time must be a number or a numeric string"},
}

// AC2: the mask sent to the SDK. The oracle is independent of dayBits (bit 0 is Sunday, as the
// SDK pkg/proxy dayNamesBitMask).
func TestDaysOfWeekArg(t *testing.T) {
	const (
		sun = 1 << iota
		mon
		tue
		wed
		thu
		fri
		sat
	)
	tests := []struct {
		value interface{}
		want  int32
	}{
		{"All", 127},
		{"ALL", 127},
		{"Weekdays", mon | tue | wed | thu | fri},
		{"mon,wed,fri", mon | wed | fri},
		{"Monday,Wednesday,Friday", mon | wed | fri},
		{" Sun , SATURDAY ", sun | sat},
		{"sun,mon,tue,wed,thu,fri,sat", 127},
		{"sunday,monday,tuesday,wednesday,thursday,friday,saturday", 127},
		{"tue,tues,tuesday", tue},
		{"thu,thurs,thursday", thu},
		{"weekdays,sat", mon | tue | wed | thu | fri | sat},
		{"sun,all", 127},
		{"fri", fri},
		{34.0, mon | fri},
		{"34", mon | fri},
		{" 34 ", mon | fri},
		{127.0, 127},
		{"127", 127},
		{1.0, sun},
	}
	for _, tt := range tests {
		got, err := (commandArgs{"d": tt.value}).daysOfWeekArg("d")
		if err != nil || got != tt.want {
			t.Errorf("daysOfWeekArg(%v) = %d, %v, want %d", tt.value, got, err, tt.want)
		}
	}
	// Every table entry resolves to the same bit as the oracle.
	for name, want := range map[string]int32{"sun": sun, "mon": mon, "tue": tue, "wed": wed, "thu": thu, "fri": fri, "sat": sat} {
		if dayBits[name] != want {
			t.Errorf("dayBits[%q] = %d, want %d", name, dayBits[name], want)
		}
	}
}

// AC1: ids are exact up to 2^53-1 as numbers, and up to 2^64-1 as strings; zero differs between
// the optional (add) and the required (remove) reader.
func TestScheduleIDArg(t *testing.T) {
	tests := []struct {
		name         string
		v            interface{}
		wantOpt      uint64
		optPresent   bool
		optReason    string
		wantRequired uint64
		reqReason    string
	}{
		{"number", 7.0, 7, true, "", 7, ""},
		{"string", "7", 7, true, "", 7, ""},
		{"2^53-1", float64(maxJSONSafeInteger), maxJSONSafeInteger, true, "", maxJSONSafeInteger, ""},
		{"2^53", float64(maxJSONSafeInteger) + 1, 0, true, "id is out of range", 0, "id is out of range"},
		{"+Inf", math.Inf(1), 0, true, "id is out of range", 0, "id is out of range"},
		{"max uint64 string", "18446744073709551615", math.MaxUint64, true, "", math.MaxUint64, ""},
		{"above uint64", "18446744073709551616", 0, true, "id is out of range", 0, "id is out of range"},
		{"zero", 0.0, 0, true, "", 0, "id must be a positive integer"},
		{"zero string", "0", 0, true, "", 0, "id must be a positive integer"},
		{"negative", -1.0, 0, true, "id must be a non-negative integer", 0, "id must be a positive integer"},
		{"fraction", 1.5, 0, true, "id must be a non-negative integer", 0, "id must be a positive integer"},
		{"NaN", math.NaN(), 0, true, "id must be a non-negative integer", 0, "id must be a positive integer"},
		{"signed string", "+7", 0, true, "id is not a valid integer", 0, "id is not a valid integer"},
		{"empty string", "", 0, true, "id is not a valid integer", 0, "id is not a valid integer"},
		{"bool", true, 0, true, "id must be a number or a numeric string", 0, "id must be a number or a numeric string"},
		{"null", nil, 0, false, "", 0, "id missing"},
	}
	check := func(t *testing.T, label string, err error, reason string) {
		t.Helper()
		if reason == "" {
			if err != nil {
				t.Errorf("%s error = %v, want nil", label, err)
			}
			return
		}
		if err == nil || err.Error() != "invalid request body: "+reason || !errors.Is(err, ErrInvalidBody) {
			t.Errorf("%s error = %v, want %q", label, err, reason)
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := commandArgs{"id": tt.v}
			id, present, err := args.optScheduleIDArg("id")
			check(t, "optScheduleIDArg", err, tt.optReason)
			if err == nil && (id != tt.wantOpt || present != tt.optPresent) {
				t.Errorf("optScheduleIDArg = %d, %t, want %d, %t", id, present, tt.wantOpt, tt.optPresent)
			}
			id, err = args.scheduleIDArg("id")
			check(t, "scheduleIDArg", err, tt.reqReason)
			if err == nil && id != tt.wantRequired {
				t.Errorf("scheduleIDArg = %d, want %d", id, tt.wantRequired)
			}
		})
	}
	if _, err := (commandArgs{}).scheduleIDArg("id"); err == nil || err.Error() != "invalid request body: id missing" {
		t.Errorf("scheduleIDArg(absent) error = %v", err)
	}
}

func TestScheduleIDsIncrease(t *testing.T) {
	now := time.Unix(1000, 0)
	source := &scheduleIDSource{now: func() time.Time { return now }}
	steps := []struct {
		advance time.Duration
		want    uint64
	}{
		{0, 1000},                 // first id: the clock
		{0, 1001},                 // same second: one more
		{0, 1002},                 // and again
		{5 * time.Second, 1005},   // the clock is ahead: the clock
		{-10 * time.Second, 1006}, // the clock went back: still increasing
		{10 * time.Second, 1007},  // still not past the last id: last + 1
		{time.Hour, 4605},         // far ahead again: the clock
	}
	for i, step := range steps {
		now = now.Add(step.advance)
		if got := source.next(); got != step.want {
			t.Fatalf("step %d: next() = %d, want %d", i, got, step.want)
		}
	}

	t.Run("concurrent", func(t *testing.T) {
		const calls = 100
		concurrent := &scheduleIDSource{now: func() time.Time { return time.Unix(1000, 0) }}
		ids := make([]uint64, calls)
		var wg sync.WaitGroup
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ids[i] = concurrent.next()
			}()
		}
		wg.Wait()
		slices.Sort(ids)
		if len(slices.Compact(ids)) != calls {
			t.Errorf("concurrent ids are not distinct: %v", ids)
		}
	})
}

// AC3: the id is generated once, in a copy, only when the body has none (absent, null or 0).
func TestPrepareCommandBody(t *testing.T) {
	const rest = `"days_of_week":"all","start_time":60,"enabled":true,"lat":1,"lon":2`
	for _, idPart := range []string{"", `"id":null,`, `"id":0,`, `"id":"0",`} {
		t.Run("generated "+idPart, func(t *testing.T) {
			freezeScheduleIDs(t, frozenScheduleUnix)
			body := decodeBody(t, "{"+idPart+rest+"}")
			original := decodeBody(t, "{"+idPart+rest+"}")

			got := PrepareCommandBody("add_charge_schedule", body)
			if got["id"] != "1767225600" {
				t.Errorf("prepared id = %#v, want \"1767225600\"", got["id"])
			}
			if !reflect.DeepEqual(body, original) {
				t.Errorf("PrepareCommandBody modified its argument: %v, was %v", body, original)
			}
			delete(got, "id")
			delete(original, "id")
			if !reflect.DeepEqual(got, original) {
				t.Errorf("prepared body = %v, want %v plus the id", got, original)
			}
			// The prepared body is valid and carries the id into the SDK message.
			schedule, err := commandArgs(PrepareCommandBody("add_charge_schedule", decodeBody(t, "{"+idPart+rest+"}"))).chargeSchedule()
			if err != nil || schedule.GetId() != frozenScheduleUnix+1 {
				t.Errorf("chargeSchedule of a second prepared body = %v, %v, want id %d", schedule, err, frozenScheduleUnix+1)
			}
		})
	}

	t.Run("an id is kept", func(t *testing.T) {
		freezeScheduleIDs(t, frozenScheduleUnix)
		body := decodeBody(t, `{"id":42,`+rest+`}`)
		got := PrepareCommandBody("add_charge_schedule", body)
		if reflect.ValueOf(got).Pointer() != reflect.ValueOf(body).Pointer() {
			t.Error("a body with an id was copied")
		}
		if got["id"] != 42.0 {
			t.Errorf("id = %#v, want 42", got["id"])
		}
		// The generator was not consumed.
		if next := scheduleIDs.next(); next != frozenScheduleUnix {
			t.Errorf("next id = %d, want %d (not consumed)", next, frozenScheduleUnix)
		}
	})

	t.Run("other commands and unknown names", func(t *testing.T) {
		body := map[string]interface{}{"id": 0.0}
		for _, name := range []string{"remove_charge_schedule", "set_scheduled_charging", "flash_lights", "unknown", "vehicle_data"} {
			got := PrepareCommandBody(name, body)
			if reflect.ValueOf(got).Pointer() != reflect.ValueOf(body).Pointer() {
				t.Errorf("PrepareCommandBody(%q) returned another map", name)
			}
		}
		if got := PrepareCommandBody("flash_lights", nil); got != nil {
			t.Errorf("PrepareCommandBody on a nil body = %v, want nil", got)
		}
	})
}

// Invariant guard: a body that was not prepared never reaches the vehicle.
func TestAddChargeScheduleUnpreparedID(t *testing.T) {
	car := &fakeCar{}
	body := decodeBody(t, `{"days_of_week":"all","start_time":60,"enabled":true,"lat":1,"lon":2}`)
	retry, err := fleetVehicleCommands["add_charge_schedule"].run(context.Background(), car, body)
	if err == nil || err.Error() != "charge schedule id was not prepared" {
		t.Errorf("run error = %v, want the not prepared error", err)
	}
	if errors.Is(err, ErrInvalidBody) {
		t.Error("the invariant error must not be reported as an invalid body")
	}
	// Assumed: the invariant error is retried like a vehicle error. The path is unreachable
	// through HTTP: handlers.Command always goes through PrepareCommandBody (D-1014-02).
	if !retry {
		t.Error("retry = false, want true (assumed, see comment)")
	}
	if len(car.calls) != 0 {
		t.Errorf("an unprepared body reached the vehicle: %v", car.calls)
	}
}

func TestCoordinateArg(t *testing.T) {
	tests := []struct {
		name   string
		args   commandArgs
		want   float64
		reason string
	}{
		{"zero is present", commandArgs{"k": 0.0}, 0, ""},
		{"upper bound", commandArgs{"k": 90.0}, 90, ""},
		{"lower bound as string", commandArgs{"k": "-90"}, -90, ""},
		{"absent", commandArgs{}, 0, "k missing"},
		{"null", commandArgs{"k": nil}, 0, "k missing"},
		{"above", commandArgs{"k": 90.5}, 0, "k must be between -90 and 90 degrees"},
		{"below", commandArgs{"k": "-91"}, 0, "k must be between -90 and 90 degrees"},
		{"NaN", commandArgs{"k": math.NaN()}, 0, "k is not a valid number"},
		{"bool", commandArgs{"k": true}, 0, "k must be a number or a numeric string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.args.coordinateArg("k", 90)
			if tt.reason == "" {
				if err != nil || got != tt.want {
					t.Errorf("coordinateArg = %v, %v, want %v", got, err, tt.want)
				}
				return
			}
			if err == nil || err.Error() != "invalid request body: "+tt.reason || !errors.Is(err, ErrInvalidBody) {
				t.Errorf("coordinateArg error = %v, want %q", err, tt.reason)
			}
		})
	}
}

// preconditionCall builds the AddPreconditionSchedule call expected for the usual cases.
func preconditionCall(id, days, minutes string) string {
	return "AddPreconditionSchedule(id=" + id + ",days=" + days + ",time=" + minutes +
		",one_time=false,enabled=true,lat=1,lon=2,name=\"\")"
}

// preconditionBodyCases are the add_precondition_schedule rows of TestCommandBodies (UC1022). The
// generated id is 1767225600 (frozen clock).
var preconditionBodyCases = []bodyCase{
	// Teslemetry body, id kept.
	{"add_precondition_schedule", `{"id":3,"days_of_week":"Weekdays","precondition_time":450,"one_time":false,"enabled":true,"lat":48.8566,"lon":2.3522,"name":"Work"}`,
		"AddPreconditionSchedule(id=3,days=0111110,time=450,one_time=false,enabled=true,lat=48.8566,lon=2.3522,name=\"\")", ""},
	// id generated when absent, null or 0; a large id as a string is kept.
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":60,"enabled":true,"lat":1,"lon":2}`,
		preconditionCall("1767225600", "1111111", "60"), ""},
	{"add_precondition_schedule", `{"id":0,"days_of_week":"all","precondition_time":60,"enabled":true,"lat":1,"lon":2}`,
		preconditionCall("1767225600", "1111111", "60"), ""},
	{"add_precondition_schedule", `{"id":null,"days_of_week":"all","precondition_time":60,"enabled":true,"lat":1,"lon":2}`,
		preconditionCall("1767225600", "1111111", "60"), ""},
	{"add_precondition_schedule", `{"id":"9007199254740993","days_of_week":"all","precondition_time":60,"enabled":true,"lat":1,"lon":2}`,
		preconditionCall("9007199254740993", "1111111", "60"), ""},
	// days_of_week forms, bounds, flags.
	{"add_precondition_schedule", `{"id":1,"days_of_week":62,"precondition_time":0,"enabled":true,"lat":1,"lon":2}`,
		preconditionCall("1", "0111110", "0"), ""},
	{"add_precondition_schedule", `{"id":1,"days_of_week":"62","precondition_time":1439,"enabled":true,"lat":1,"lon":2}`,
		preconditionCall("1", "0111110", "1439"), ""},
	{"add_precondition_schedule", `{"id":1,"days_of_week":" Sun , SATURDAY ","precondition_time":"450","enabled":true,"lat":"1","lon":"2"}`,
		preconditionCall("1", "1000001", "450"), ""},
	{"add_precondition_schedule", `{"id":1,"days_of_week":"all","precondition_time":60,"one_time":true,"enabled":false,"lat":-33.86,"lon":151.2}`,
		"AddPreconditionSchedule(id=1,days=1111111,time=60,one_time=true,enabled=false,lat=-33.86,lon=151.2,name=\"\")", ""},

	// Refused (the first faulty field wins, in the order of preconditionSchedule).
	{"add_precondition_schedule", ``, "", "invalid request body: days_of_week missing"},
	{"add_precondition_schedule", `{}`, "", "invalid request body: days_of_week missing"},
	{"add_precondition_schedule", `{"id":-1,"days_of_week":"x"}`, "", "invalid request body: id must be a non-negative integer"},
	{"add_precondition_schedule", `{"days_of_week":"x","precondition_time":"abc"}`, "", "invalid request body: days_of_week contains an unknown day name"},
	{"add_precondition_schedule", `{"days_of_week":"all"}`, "", "invalid request body: precondition_time missing"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":1440}`, "", "invalid request body: precondition_time must be an integer between 0 and 1439"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":-1}`, "", "invalid request body: precondition_time must be an integer between 0 and 1439"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":450.5}`, "", "invalid request body: precondition_time must be an integer between 0 and 1439"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":"07:30"}`, "", "invalid request body: precondition_time is not a valid integer"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":60,"one_time":"maybe"}`, "", "invalid request body: one_time is not a valid boolean"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":60,"lat":1,"lon":2}`, "", "invalid request body: enabled missing"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":60,"enabled":true,"lon":2}`, "", "invalid request body: lat missing"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":60,"enabled":true,"lat":1}`, "", "invalid request body: lon missing"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":60,"enabled":true,"lat":91,"lon":2}`, "", "invalid request body: lat must be between -90 and 90 degrees"},
	{"add_precondition_schedule", `{"days_of_week":"all","precondition_time":60,"enabled":true,"lat":1,"lon":-181}`, "", "invalid request body: lon must be between -180 and 180 degrees"},
}

// departureBodyCases are the set_scheduled_departure rows of TestCommandBodies (UC1022, D-1022-04, D-1022-09).
var departureBodyCases = []bodyCase{
	// Preconditioning only, all days; weekdays only.
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"preconditioning_enabled":true}`,
		"ScheduleDeparture(7h30m0s,0s,AllDays,Off)", ""},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"preconditioning_enabled":true,"preconditioning_weekdays_only":true}`,
		"ScheduleDeparture(7h30m0s,0s,Weekdays,Off)", ""},
	// Off-peak only, then both.
	{"set_scheduled_departure", `{"enable":true,"departure_time":420,"off_peak_charging_enabled":true,"end_off_peak_time":360}`,
		"ScheduleDeparture(7h0m0s,6h0m0s,Off,AllDays)", ""},
	{"set_scheduled_departure", `{"enable":true,"departure_time":420,"preconditioning_enabled":true,"off_peak_charging_enabled":true,"off_peak_charging_weekdays_only":true,"end_off_peak_time":360}`,
		"ScheduleDeparture(7h0m0s,6h0m0s,AllDays,Weekdays)", ""},
	{"set_scheduled_departure", `{"enable":"true","departure_time":"1439","preconditioning_enabled":"1","off_peak_charging_enabled":"true","end_off_peak_time":"0"}`,
		"ScheduleDeparture(23h59m0s,0s,AllDays,AllDays)", ""},
	// end_off_peak_time is transmitted even with off-peak charging disabled.
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"preconditioning_enabled":true,"end_off_peak_time":300}`,
		"ScheduleDeparture(7h30m0s,5h0m0s,AllDays,Off)", ""},
	// Departure alone, and weekdays_only without its enabled flag (ignored, D-1022-04).
	{"set_scheduled_departure", `{"enable":true,"departure_time":450}`, "ScheduleDeparture(7h30m0s,0s,Off,Off)", ""},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"preconditioning_weekdays_only":true}`,
		"ScheduleDeparture(7h30m0s,0s,Off,Off)", ""},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"off_peak_charging_weekdays_only":true}`,
		"ScheduleDeparture(7h30m0s,0s,Off,Off)", ""},
	// enable false clears, whatever the valid rest (D-1022-09).
	{"set_scheduled_departure", `{"enable":false}`, "ClearScheduledDeparture", ""},
	{"set_scheduled_departure", `{"enable":false,"departure_time":450,"preconditioning_enabled":true,"preconditioning_weekdays_only":true,"off_peak_charging_enabled":true,"end_off_peak_time":360}`,
		"ClearScheduledDeparture", ""},

	// Refused.
	{"set_scheduled_departure", ``, "", "invalid request body: enable missing"},
	{"set_scheduled_departure", `{}`, "", "invalid request body: enable missing"},
	{"set_scheduled_departure", `{"enable":"yes"}`, "", "invalid request body: enable is not a valid boolean"},
	{"set_scheduled_departure", `{"enable":true}`, "", "invalid request body: departure_time missing"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":null}`, "", "invalid request body: departure_time missing"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"off_peak_charging_enabled":true}`, "", "invalid request body: end_off_peak_time missing"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":1440}`, "", "invalid request body: departure_time must be an integer between 0 and 1439"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":-1}`, "", "invalid request body: departure_time must be an integer between 0 and 1439"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450.5}`, "", "invalid request body: departure_time must be an integer between 0 and 1439"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":"07:30"}`, "", "invalid request body: departure_time is not a valid integer"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"end_off_peak_time":1440}`, "", "invalid request body: end_off_peak_time must be an integer between 0 and 1439"},
	{"set_scheduled_departure", `{"enable":true,"preconditioning_enabled":1,"departure_time":2000}`, "", `invalid request body: preconditioning_enabled must be a boolean or "true"/"false"`},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"preconditioning_weekdays_only":"maybe"}`, "", "invalid request body: preconditioning_weekdays_only is not a valid boolean"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"off_peak_charging_enabled":"maybe"}`, "", "invalid request body: off_peak_charging_enabled is not a valid boolean"},
	{"set_scheduled_departure", `{"enable":true,"departure_time":450,"off_peak_charging_weekdays_only":"maybe"}`, "", "invalid request body: off_peak_charging_weekdays_only is not a valid boolean"},
	{"set_scheduled_departure", `{"enable":false,"departure_time":"abc"}`, "", "invalid request body: departure_time is not a valid integer"},
	{"set_scheduled_departure", `{"enable":false,"end_off_peak_time":1440}`, "", "invalid request body: end_off_peak_time must be an integer between 0 and 1439"},
}

// departurePolicy: weekdays_only is ignored when the policy is disabled.
func TestDeparturePolicy(t *testing.T) {
	tests := []struct {
		enabled, weekdaysOnly bool
		want                  string
	}{
		{false, false, "Off"},
		{false, true, "Off"},
		{true, false, "AllDays"},
		{true, true, "Weekdays"},
	}
	for _, tt := range tests {
		if got := sdkPolicyName(departurePolicy(tt.enabled, tt.weekdaysOnly)); got != tt.want {
			t.Errorf("departurePolicy(%t, %t) = %s, want %s", tt.enabled, tt.weekdaysOnly, got, tt.want)
		}
	}
}

// add_precondition_schedule refuses to reach the vehicle with an id that was not prepared.
func TestAddPreconditionScheduleUnpreparedID(t *testing.T) {
	car := &fakeCar{}
	body := decodeBody(t, `{"days_of_week":"all","precondition_time":60,"enabled":true,"lat":1,"lon":2}`)
	retry, err := fleetVehicleCommands["add_precondition_schedule"].run(context.Background(), car, body)
	if err == nil || err.Error() != "precondition schedule id was not prepared" || errors.Is(err, ErrInvalidBody) || len(car.calls) != 0 {
		t.Errorf("run = (%t, %v), calls %v, want the not prepared error without vehicle call", retry, err, car.calls)
	}
}

// UC1022: the precondition id is generated once on a copy; the charge and precondition schedules
// share the generator; the other commands of the UC are left untouched.
func TestPreparePreconditionBody(t *testing.T) {
	freezeScheduleIDs(t, frozenScheduleUnix)
	for i, idPart := range []string{"", `"id":null,`, `"id":0,`, `"id":"0",`} {
		body := decodeBody(t, `{`+idPart+`"days_of_week":"all","precondition_time":60,"enabled":true,"lat":1,"lon":2}`)
		original := maps.Clone(body)
		prepared := PrepareCommandBody("add_precondition_schedule", body)
		wantID := strconv.FormatUint(frozenScheduleUnix+uint64(i), 10)
		if prepared["id"] != wantID {
			t.Errorf("%q: prepared id = %#v, want %q", idPart, prepared["id"], wantID)
		}
		rest := maps.Clone(prepared)
		delete(rest, "id")
		wantRest := maps.Clone(original)
		delete(wantRest, "id")
		if !reflect.DeepEqual(rest, wantRest) {
			t.Errorf("%q: other keys changed: %v, want %v", idPart, rest, wantRest)
		}
		if !reflect.DeepEqual(body, original) {
			t.Errorf("%q: PrepareCommandBody modified its argument: %v", idPart, body)
		}
	}

	withID := decodeBody(t, `{"id":42,"days_of_week":"all","precondition_time":60,"enabled":true,"lat":1,"lon":2}`)
	if got := PrepareCommandBody("add_precondition_schedule", withID); !reflect.DeepEqual(got, withID) {
		t.Errorf("a body with an id changed: %v", got)
	}

	freezeScheduleIDs(t, frozenScheduleUnix)
	charge := PrepareCommandBody("add_charge_schedule", decodeBody(t, `{"days_of_week":"all","start_time":1,"enabled":true,"lat":1,"lon":2}`))
	precondition := PrepareCommandBody("add_precondition_schedule", decodeBody(t, `{"days_of_week":"all","precondition_time":1,"enabled":true,"lat":1,"lon":2}`))
	if charge["id"] != "1767225600" || precondition["id"] != "1767225601" {
		t.Errorf("shared generator: charge id %v, precondition id %v, want 1767225600 then 1767225601", charge["id"], precondition["id"])
	}

	for _, name := range []string{"charge_max_range", "charge_standard", "schedule_software_update", "cancel_software_update",
		"adjust_volume", "media_toggle_playback", "remove_precondition_schedule", "set_scheduled_departure"} {
		in := map[string]interface{}{"id": 0.0}
		if got := PrepareCommandBody(name, in); !reflect.DeepEqual(got, in) {
			t.Errorf("PrepareCommandBody(%q) changed the body: %v", name, got)
		}
	}
}
