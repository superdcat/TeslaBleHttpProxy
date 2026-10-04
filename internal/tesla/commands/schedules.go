// Charge schedule commands (UC1014): add_charge_schedule, remove_charge_schedule and
// set_scheduled_charging; UC1022 adds add_precondition_schedule and set_scheduled_departure.
// The body parsers live here, the registry entries in fleetVehicleCommands.go.
//
// Derived from Lenart12/TeslaBleHttpProxy, internal/tesla/commands/fleetVehicleCommands.go
// (commit 8bf65b8, author skrul, merged by c68b8ee), Copyright Lenart12 and contributors,
// Apache License 2.0.
// Modified in the superdcat fork: strictly typed body following the contract of wimaha
// PR #153, days of the week as the union of the SDK (pkg/proxy), Lenart12 and PR #153, id
// generated once when the command is queued, required fields and bounds checked before queuing.

package commands

import (
	"errors"
	"maps"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
)

const (
	// maxMinuteOfDay is the last minute after midnight accepted for a schedule time.
	maxMinuteOfDay = 1439
	// maxJSONSafeInteger is the largest integer a JSON number carries exactly (2^53-1); larger
	// ids must be sent as decimal strings.
	maxJSONSafeInteger = 1<<53 - 1
	// allDaysMask is the days_of_week mask of all seven days; weekdaysMask is Monday to Friday.
	allDaysMask  = 127
	weekdaysMask = 62
)

// dayBits maps a days_of_week token (lower case) to its bit. Bit 0 is Sunday, then Monday (2)
// to Saturday (64), as vehicle-command pkg/proxy/command.go (dayNamesBitMask). Accepted tokens
// are the union of the SDK, Lenart12 (sun..sat) and wimaha PR #153 (full names, All, Weekdays).
var dayBits = map[string]int32{
	"sun": 1 << 0, "sunday": 1 << 0,
	"mon": 1 << 1, "monday": 1 << 1,
	"tue": 1 << 2, "tues": 1 << 2, "tuesday": 1 << 2,
	"wed": 1 << 3, "wednesday": 1 << 3,
	"thu": 1 << 4, "thurs": 1 << 4, "thursday": 1 << 4,
	"fri": 1 << 5, "friday": 1 << 5,
	"sat": 1 << 6, "saturday": 1 << 6,
	"all":      allDaysMask,
	"weekdays": weekdaysMask,
}

// optIntRangeArg reads an optional integer in [lo, hi] with the rules of intRangeArg: absent or
// null gives (0, false, nil).
func (args commandArgs) optIntRangeArg(key string, lo, hi int) (value int, present bool, err error) {
	if args[key] == nil {
		return 0, false, nil
	}
	value, err = args.intRangeArg(key, lo, hi)
	return value, true, err
}

// daysOfWeekArg reads the required days_of_week: a comma separated list of day names (case
// insensitive, spaces around a name ignored, All and Weekdays mixable) or a bitmask between 1 and
// 127, as a JSON number or a numeric string.
func (args commandArgs) daysOfWeekArg(key string) (int32, error) {
	const bitmaskReason = "%s must be a bitmask between 1 and 127"
	switch v := args[key].(type) {
	case nil:
		return 0, invalidBodyf("%s missing", key)
	case float64:
		if v != math.Trunc(v) || v < 1 || v > allDaysMask {
			return 0, invalidBodyf(bitmaskReason, key)
		}
		return int32(v), nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil || errors.Is(err, strconv.ErrRange) {
			if err != nil || n < 1 || n > allDaysMask {
				return 0, invalidBodyf(bitmaskReason, key)
			}
			return int32(n), nil
		}
		// ToLower and not EqualFold, so that the long s, U+017F, does not match "s".
		var mask int32
		for _, name := range strings.Split(v, ",") {
			bit, ok := dayBits[strings.ToLower(strings.TrimSpace(name))]
			if !ok {
				return 0, invalidBodyf("%s contains an unknown day name", key)
			}
			mask |= bit
		}
		return mask, nil
	default:
		return 0, invalidBodyf("%s must be a string or a number", key)
	}
}

// scheduleIDValue reads a schedule id: a JSON number (an integer up to 2^53-1) or a decimal
// string (an unsigned 64-bit integer). nounPhrase completes "<key> must be " for a negative or
// fractional number. Zero is not refused here.
func (args commandArgs) scheduleIDValue(key, nounPhrase string) (id uint64, present bool, err error) {
	switch v := args[key].(type) {
	case nil:
		return 0, false, nil
	case float64:
		if math.IsNaN(v) || v < 0 || v != math.Trunc(v) {
			return 0, true, invalidBodyf("%s must be %s", key, nounPhrase)
		}
		if v > maxJSONSafeInteger {
			return 0, true, invalidBodyf("%s is out of range", key)
		}
		return uint64(v), true, nil
	case string:
		n, perr := strconv.ParseUint(v, 10, 64)
		if errors.Is(perr, strconv.ErrRange) {
			return 0, true, invalidBodyf("%s is out of range", key)
		}
		if perr != nil {
			return 0, true, invalidBodyf("%s is not a valid integer", key)
		}
		return n, true, nil
	default:
		return 0, true, invalidBodyf("%s must be a number or a numeric string", key)
	}
}

// optScheduleIDArg reads an optional id >= 0 (add_charge_schedule: absent, null or 0 means
// "to be generated").
func (args commandArgs) optScheduleIDArg(key string) (id uint64, present bool, err error) {
	return args.scheduleIDValue(key, "a non-negative integer")
}

// scheduleIDArg reads the required id >= 1 (remove_charge_schedule): protobuf 3 does not send a
// zero uint64, so the vehicle would receive an empty action.
func (args commandArgs) scheduleIDArg(key string) (uint64, error) {
	const reason = "a positive integer"
	id, present, err := args.scheduleIDValue(key, reason)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, invalidBodyf("%s missing", key)
	}
	if id == 0 {
		return 0, invalidBodyf("%s must be %s", key, reason)
	}
	return id, nil
}

// scheduleSwitch reads one end of a schedule: the optional switch flagKey (absent or null: on when
// timeKey is present) and the time timeKey in minutes after midnight (required when the switch is
// on, transmitted even when it is off).
func (args commandArgs) scheduleSwitch(flagKey, timeKey string) (enabled bool, minutes int32, err error) {
	if args[flagKey] == nil {
		enabled = args[timeKey] != nil
	} else if enabled, err = args.boolArg(flagKey); err != nil {
		return false, 0, err
	}
	m, present, err := args.optIntRangeArg(timeKey, 0, maxMinuteOfDay)
	if err != nil {
		return false, 0, err
	}
	if enabled && !present {
		return false, 0, invalidBodyf("%s missing", timeKey)
	}
	return enabled, int32(m), nil
}

// chargeSchedule reads and validates the body of add_charge_schedule. The Id is 0 while the body
// has none (see prepareChargeSchedule). It never modifies args and returns a new message.
func (args commandArgs) chargeSchedule() (*vehicle.ChargeSchedule, error) {
	id, _, err := args.optScheduleIDArg("id")
	if err != nil {
		return nil, err
	}
	days, err := args.daysOfWeekArg("days_of_week")
	if err != nil {
		return nil, err
	}
	startEnabled, startTime, err := args.scheduleSwitch("start_enabled", "start_time")
	if err != nil {
		return nil, err
	}
	endEnabled, endTime, err := args.scheduleSwitch("end_enabled", "end_time")
	if err != nil {
		return nil, err
	}
	if !startEnabled && !endEnabled {
		return nil, invalidBodyf("start_enabled or end_enabled must be true")
	}
	oneTime, err := args.optBoolArg("one_time")
	if err != nil {
		return nil, err
	}
	enabled, err := args.boolArg("enabled")
	if err != nil {
		return nil, err
	}
	lat, err := args.coordinateArg("lat", maxLatitude)
	if err != nil {
		return nil, err
	}
	lon, err := args.coordinateArg("lon", maxLongitude)
	if err != nil {
		return nil, err
	}
	return &vehicle.ChargeSchedule{
		Id:           id,
		DaysOfWeek:   days,
		StartEnabled: startEnabled,
		StartTime:    startTime,
		EndEnabled:   endEnabled,
		EndTime:      endTime,
		OneTime:      oneTime,
		Enabled:      enabled,
		Latitude:     float32(lat),
		Longitude:    float32(lon),
	}, nil
}

// prepareChargeSchedule gives a body without id (absent, null or 0) a generated one, as a decimal
// string (exact, and readable in the logs). It returns a copy; a body that carries an id is
// returned as is. Called once per request by PrepareCommandBody, never by validate or execute,
// so that every retry replays the same id.
func prepareChargeSchedule(args commandArgs) commandArgs {
	return withGeneratedScheduleID(args, "add_charge_schedule", "Generated charge schedule id")
}

// preparePreconditionSchedule is prepareChargeSchedule for add_precondition_schedule.
func preparePreconditionSchedule(args commandArgs) commandArgs {
	return withGeneratedScheduleID(args, "add_precondition_schedule", "Generated precondition schedule id")
}

// withGeneratedScheduleID returns a copy of args with a generated "id" when the body has none
// (absent, null or 0), otherwise args itself. A body whose id is unreadable is returned as is:
// validate refuses it.
func withGeneratedScheduleID(args commandArgs, command, message string) commandArgs {
	if id, _, err := args.optScheduleIDArg("id"); err != nil || id != 0 {
		return args
	}
	id := scheduleIDs.next()
	logging.Info(message, "Command", command, "Id", id)
	prepared := make(commandArgs, len(args)+1)
	maps.Copy(prepared, args)
	prepared["id"] = strconv.FormatUint(id, 10)
	return prepared
}

// preconditionSchedule reads and validates the body of add_precondition_schedule: id (optional),
// days_of_week, precondition_time (minutes after midnight), one_time (optional), enabled, lat and
// lon (required). name is ignored. The Id is 0 while the body has none (see
// preparePreconditionSchedule). It never modifies args and returns a new message.
func (args commandArgs) preconditionSchedule() (*vehicle.PreconditionSchedule, error) {
	id, _, err := args.optScheduleIDArg("id")
	if err != nil {
		return nil, err
	}
	days, err := args.daysOfWeekArg("days_of_week")
	if err != nil {
		return nil, err
	}
	minutes, err := args.intRangeArg("precondition_time", 0, maxMinuteOfDay)
	if err != nil {
		return nil, err
	}
	oneTime, err := args.optBoolArg("one_time")
	if err != nil {
		return nil, err
	}
	enabled, err := args.boolArg("enabled")
	if err != nil {
		return nil, err
	}
	lat, err := args.coordinateArg("lat", maxLatitude)
	if err != nil {
		return nil, err
	}
	lon, err := args.coordinateArg("lon", maxLongitude)
	if err != nil {
		return nil, err
	}
	return &vehicle.PreconditionSchedule{
		Id:               id,
		DaysOfWeek:       days,
		PreconditionTime: int32(minutes),
		OneTime:          oneTime,
		Enabled:          enabled,
		Latitude:         float32(lat),
		Longitude:        float32(lon),
	}, nil
}

// scheduledCharging reads the body of set_scheduled_charging: enable (required) and time in
// minutes after midnight (required when enable is true; optional otherwise, 0 when absent).
func (args commandArgs) scheduledCharging() (enable bool, minutes int, err error) {
	enable, err = args.boolArg("enable")
	if err != nil {
		return false, 0, err
	}
	minutes, present, err := args.optIntRangeArg("time", 0, maxMinuteOfDay)
	if err != nil {
		return false, 0, err
	}
	if enable && !present {
		return false, 0, invalidBodyf("time missing")
	}
	return enable, minutes, nil
}

// departure is the validated body of set_scheduled_departure. With enable false only enable
// is meaningful (ClearScheduledDeparture takes no argument).
type departure struct {
	enable                   bool
	departAt, offPeakEnd     time.Duration
	preconditioning, offPeak vehicle.ChargingPolicy
}

// scheduledDeparture reads the body of set_scheduled_departure. Types and ranges of every field
// present are always checked, even when enable is false (the other fields are then ignored, as
// set_scheduled_charging checks time). With enable true, departure_time is required, and
// end_off_peak_time when off-peak charging is enabled; it is transmitted whenever present.
func (args commandArgs) scheduledDeparture() (departure, error) {
	enable, err := args.boolArg("enable")
	if err != nil {
		return departure{}, err
	}
	preEnabled, err := args.optBoolArg("preconditioning_enabled")
	if err != nil {
		return departure{}, err
	}
	preWeekdays, err := args.optBoolArg("preconditioning_weekdays_only")
	if err != nil {
		return departure{}, err
	}
	departMinutes, departPresent, err := args.optIntRangeArg("departure_time", 0, maxMinuteOfDay)
	if err != nil {
		return departure{}, err
	}
	offEnabled, err := args.optBoolArg("off_peak_charging_enabled")
	if err != nil {
		return departure{}, err
	}
	offWeekdays, err := args.optBoolArg("off_peak_charging_weekdays_only")
	if err != nil {
		return departure{}, err
	}
	offEndMinutes, offEndPresent, err := args.optIntRangeArg("end_off_peak_time", 0, maxMinuteOfDay)
	if err != nil {
		return departure{}, err
	}
	if !enable {
		return departure{}, nil
	}
	if !departPresent {
		return departure{}, invalidBodyf("departure_time missing")
	}
	if offEnabled && !offEndPresent {
		return departure{}, invalidBodyf("end_off_peak_time missing")
	}
	return departure{
		enable:          true,
		departAt:        time.Duration(departMinutes) * time.Minute,
		offPeakEnd:      time.Duration(offEndMinutes) * time.Minute,
		preconditioning: departurePolicy(preEnabled, preWeekdays),
		offPeak:         departurePolicy(offEnabled, offWeekdays),
	}, nil
}

// departurePolicy maps a Fleet enabled / weekdays_only pair to the SDK policy. weekdaysOnly is
// ignored when enabled is false: the client asked for "off". This differs on purpose from
// pkg/proxy getPolicy of the official proxy, where weekdays_only alone means Weekdays.
func departurePolicy(enabled, weekdaysOnly bool) vehicle.ChargingPolicy {
	switch {
	case !enabled:
		return vehicle.ChargingPolicyOff
	case weekdaysOnly:
		return vehicle.ChargingPolicyWeekdays
	default:
		return vehicle.ChargingPolicyAllDays
	}
}

// scheduleIDSource generates schedule ids: Unix seconds, the Fleet convention ("datetime in epoch
// time"), strictly increasing so that two ids generated in the same second differ.
type scheduleIDSource struct {
	mu   sync.Mutex
	last uint64
	now  func() time.Time
}

// next returns max(now in Unix seconds, last + 1).
func (s *scheduleIDSource) next() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id uint64
	if unix := s.now().Unix(); unix > 0 {
		id = uint64(unix)
	}
	if id <= s.last {
		id = s.last + 1
	}
	s.last = id
	return id
}

var scheduleIDs = &scheduleIDSource{now: time.Now}
