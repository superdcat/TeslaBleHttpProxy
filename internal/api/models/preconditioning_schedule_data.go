package models

import (
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
)

// PreconditionScheduleEntry is one preconditioning schedule of the vehicle. Written for the
// superdcat fork. days_of_week is the raw bitmask (bit 0 = Sunday ... 64 = Saturday) and
// precondition_time is minutes after midnight in the vehicle's local time. latitude and
// longitude are the location of the schedule: personal data, never to be logged. Every field is
// always present: a value the vehicle does not report is 0, false or "".
type PreconditionScheduleEntry struct {
	ID               uint64  `json:"id"`
	Name             string  `json:"name"`
	DaysOfWeek       int32   `json:"days_of_week"`      // bitmask, bit 0 = Sunday ... 64 = Saturday
	PreconditionTime int32   `json:"precondition_time"` // minutes after midnight, vehicle local time
	OneTime          bool    `json:"one_time"`
	Enabled          bool    `json:"enabled"`
	Latitude         float32 `json:"latitude"`
	Longitude        float32 `json:"longitude"`
}

// PreconditioningScheduleData contains the preconditioning schedules of the vehicle
// (PreconditioningScheduleState). precondition_schedules is never null: it is [] when the vehicle
// reports none. preconditioning_schedule_window and next_schedule are served as the vehicle
// sends them, their meaning is not documented; a window with id 0 means "not reported".
type PreconditioningScheduleData struct {
	Timestamp                     int64                       `json:"timestamp"`
	PreconditionSchedules         []PreconditionScheduleEntry `json:"precondition_schedules"`          // never nil: [] when empty
	PreconditioningScheduleWindow PreconditionScheduleEntry   `json:"preconditioning_schedule_window"` // zero form (id 0) when not reported
	MaxNumPreconditionSchedules   uint32                      `json:"max_num_precondition_schedules"`
	NextSchedule                  bool                        `json:"next_schedule"`
}

// PreconditioningScheduleDataFromBle converts the preconditioning schedule state of the vehicle
// data; a nil argument or a missing state gives the zero form.
func PreconditioningScheduleDataFromBle(vehicleData *carserver.VehicleData) PreconditioningScheduleData {
	s := vehicleData.GetPreconditioningScheduleState()
	list := s.GetPreconditionSchedules()
	schedules := make([]PreconditionScheduleEntry, 0, len(list))
	for _, e := range list {
		schedules = append(schedules, preconditionScheduleEntry(e))
	}
	return PreconditioningScheduleData{
		Timestamp:                     s.GetTimestamp().GetSeconds(),
		PreconditionSchedules:         schedules,
		PreconditioningScheduleWindow: preconditionScheduleEntry(s.GetPreconditioningScheduleWindow()),
		MaxNumPreconditionSchedules:   s.GetMaxNumPreconditionSchedules(),
		NextSchedule:                  s.GetNextSchedule(),
	}
}

// preconditionScheduleEntry converts one schedule; nil gives the zero entry.
func preconditionScheduleEntry(s *carserver.PreconditionSchedule) PreconditionScheduleEntry {
	return PreconditionScheduleEntry{
		ID:               s.GetId(),
		Name:             s.GetName(),
		DaysOfWeek:       s.GetDaysOfWeek(),
		PreconditionTime: s.GetPreconditionTime(),
		OneTime:          s.GetOneTime(),
		Enabled:          s.GetEnabled(),
		Latitude:         finite32(s.GetLatitude()),
		Longitude:        finite32(s.GetLongitude()),
	}
}
