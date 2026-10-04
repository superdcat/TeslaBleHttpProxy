package models

import (
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
)

// SoftwareUpdate contains the state of the vehicle software update. Written for the superdcat
// fork. Every field is always present: a value the vehicle does not report is 0 or "", except
// status, which is "Unknown". The *_ms fields are milliseconds as sent by the vehicle;
// timestamp is in Unix seconds.
type SoftwareUpdate struct {
	Timestamp              int64  `json:"timestamp"`
	Status                 string `json:"status"`
	ScheduledTimeMs        uint64 `json:"scheduled_time_ms"`
	WarningTimeRemainingMs uint64 `json:"warning_time_remaining_ms"`
	ExpectedDurationSec    uint32 `json:"expected_duration_sec"`
	DownloadPerc           uint32 `json:"download_perc"`
	InstallPerc            uint32 `json:"install_perc"`
	Version                string `json:"version"`
}

// SoftwareUpdateFromBle converts the software update state of the vehicle data; a nil argument
// or a missing software update state gives the zero form.
func SoftwareUpdateFromBle(vehicleData *carserver.VehicleData) SoftwareUpdate {
	s := vehicleData.GetSoftwareUpdateState()
	return SoftwareUpdate{
		Timestamp:              s.GetTimestamp().GetSeconds(),
		Status:                 softwareUpdateStatus(s.GetStatus()),
		ScheduledTimeMs:        s.GetScheduledTimeMs(),
		WarningTimeRemainingMs: s.GetWarningTimeRemainingMs(),
		ExpectedDurationSec:    s.GetExpectedDurationSec(),
		DownloadPerc:           s.GetDownloadPerc(),
		InstallPerc:            s.GetInstallPerc(),
		Version:                s.GetVersion(),
	}
}

// softwareUpdateStatus names the status member (Unknown, Installing, Scheduled, Available,
// DownloadingWifiWait, Downloading, and any member the SDK adds later). No status at all (absent
// message, empty oneof) gives "Unknown" (UC1017 AC2), the value the protocol itself has for it.
func softwareUpdateStatus(s *carserver.SoftwareUpdateState_SoftwareUpdateStatus) string {
	if s.GetType() == nil {
		return "Unknown"
	}
	return flatten(s.String())
}
