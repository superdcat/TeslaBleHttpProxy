package models

import (
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
)

// ChargeScheduleEntry is one charge schedule of the vehicle. Written for the superdcat fork.
// days_of_week is the raw bitmask (bit 0 = Sunday ... 64 = Saturday, 127 = every day), start_time
// and end_time are minutes after midnight in the vehicle's local time, as the vehicle sends them.
// latitude and longitude are the location of the schedule: personal data, never to be logged.
// Every field is always present: a value the vehicle does not report is 0, false or "".
type ChargeScheduleEntry struct {
	ID           uint64  `json:"id"`
	Name         string  `json:"name"`
	DaysOfWeek   int32   `json:"days_of_week"` // bitmask, bit 0 = Sunday ... 64 = Saturday
	StartEnabled bool    `json:"start_enabled"`
	StartTime    int32   `json:"start_time"` // minutes after midnight, vehicle local time
	EndEnabled   bool    `json:"end_enabled"`
	EndTime      int32   `json:"end_time"`
	OneTime      bool    `json:"one_time"`
	Enabled      bool    `json:"enabled"`
	Latitude     float32 `json:"latitude"`
	Longitude    float32 `json:"longitude"`
}

// ChargeScheduleData contains the charge schedules of the vehicle (ChargeScheduleState).
// charge_schedules is never null: it is [] when the vehicle reports none. charge_schedule_window,
// charge_buffer, next_schedule and show_schedule_complete_state are served as the vehicle sends
// them, their meaning is not documented; a window with id 0 means "not reported".
type ChargeScheduleData struct {
	Timestamp                 int64                 `json:"timestamp"`
	ChargeSchedules           []ChargeScheduleEntry `json:"charge_schedules"`       // never nil: [] when empty
	ChargeScheduleWindow      ChargeScheduleEntry   `json:"charge_schedule_window"` // zero form (id 0) when not reported
	ChargeBuffer              int32                 `json:"charge_buffer"`
	MaxNumChargeSchedules     uint32                `json:"max_num_charge_schedules"`
	NextSchedule              bool                  `json:"next_schedule"`
	ShowScheduleCompleteState bool                  `json:"show_schedule_complete_state"`
}

// ChargeScheduleDataFromBle converts the charge schedule state of the vehicle data; a nil
// argument or a missing state gives the zero form.
func ChargeScheduleDataFromBle(vehicleData *carserver.VehicleData) ChargeScheduleData {
	s := vehicleData.GetChargeScheduleState()
	list := s.GetChargeSchedules()
	schedules := make([]ChargeScheduleEntry, 0, len(list))
	for _, e := range list {
		schedules = append(schedules, chargeScheduleEntry(e))
	}
	return ChargeScheduleData{
		Timestamp:                 s.GetTimestamp().GetSeconds(),
		ChargeSchedules:           schedules,
		ChargeScheduleWindow:      chargeScheduleEntry(s.GetChargeScheduleWindow()),
		ChargeBuffer:              s.GetChargeBuffer(),
		MaxNumChargeSchedules:     s.GetMaxNumChargeSchedules(),
		NextSchedule:              s.GetNextSchedule(),
		ShowScheduleCompleteState: s.GetShowScheduleCompleteState(),
	}
}

// chargeScheduleEntry converts one schedule; nil gives the zero entry.
func chargeScheduleEntry(s *carserver.ChargeSchedule) ChargeScheduleEntry {
	return ChargeScheduleEntry{
		ID:           s.GetId(),
		Name:         s.GetName(),
		DaysOfWeek:   s.GetDaysOfWeek(),
		StartEnabled: s.GetStartEnabled(),
		StartTime:    s.GetStartTime(),
		EndEnabled:   s.GetEndEnabled(),
		EndTime:      s.GetEndTime(),
		OneTime:      s.GetOneTime(),
		Enabled:      s.GetEnabled(),
		Latitude:     finite32(s.GetLatitude()),
		Longitude:    finite32(s.GetLongitude()),
	}
}
