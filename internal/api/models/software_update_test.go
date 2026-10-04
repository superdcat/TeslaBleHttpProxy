package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const softwareUpdateZero = `{"timestamp":0,"status":"Unknown","scheduled_time_ms":0,"warning_time_remaining_ms":0,` +
	`"expected_duration_sec":0,"download_perc":0,"install_perc":0,"version":""}`

func softwareUpdateJSON(t *testing.T, s *carserver.SoftwareUpdateState) string {
	t.Helper()
	return mustJSON(t, SoftwareUpdateFromBle(&carserver.VehicleData{SoftwareUpdateState: s}))
}

// AC2: a download at 40 % is converted with every expected key, in order.
func TestSoftwareUpdateJSON(t *testing.T) {
	s := &carserver.SoftwareUpdateState{
		Status:                      &carserver.SoftwareUpdateState_SoftwareUpdateStatus{Type: &carserver.SoftwareUpdateState_SoftwareUpdateStatus_Downloading{Downloading: &carserver.Void{}}},
		OptionalExpectedDurationSec: &carserver.SoftwareUpdateState_ExpectedDurationSec{ExpectedDurationSec: 1500},
		OptionalDownloadPerc:        &carserver.SoftwareUpdateState_DownloadPerc{DownloadPerc: 40},
		OptionalVersion:             &carserver.SoftwareUpdateState_Version{Version: "2026.32.5"},
		Timestamp:                   &timestamppb.Timestamp{Seconds: 1767254400},
	}
	want := `{"timestamp":1767254400,"status":"Downloading","scheduled_time_ms":0,"warning_time_remaining_ms":0,` +
		`"expected_duration_sec":1500,"download_perc":40,"install_perc":0,"version":"2026.32.5"}`
	if got := softwareUpdateJSON(t, s); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// The six statuses of the protocol; no status at all gives Unknown.
func TestSoftwareUpdateStatus(t *testing.T) {
	v := &carserver.Void{}
	type st = carserver.SoftwareUpdateState_SoftwareUpdateStatus
	tests := []struct {
		name   string
		status *st
		want   string
	}{
		{"Unknown", &st{Type: &carserver.SoftwareUpdateState_SoftwareUpdateStatus_Unknown{Unknown: v}}, "Unknown"},
		{"Installing", &st{Type: &carserver.SoftwareUpdateState_SoftwareUpdateStatus_Installing{Installing: v}}, "Installing"},
		{"Scheduled", &st{Type: &carserver.SoftwareUpdateState_SoftwareUpdateStatus_Scheduled{Scheduled: v}}, "Scheduled"},
		{"Available", &st{Type: &carserver.SoftwareUpdateState_SoftwareUpdateStatus_Available{Available: v}}, "Available"},
		{"DownloadingWifiWait", &st{Type: &carserver.SoftwareUpdateState_SoftwareUpdateStatus_DownloadingWifiWait{DownloadingWifiWait: v}}, "DownloadingWifiWait"},
		{"Downloading", &st{Type: &carserver.SoftwareUpdateState_SoftwareUpdateStatus_Downloading{Downloading: v}}, "Downloading"},
		{"empty message", &st{}, "Unknown"},
		{"absent", nil, "Unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SoftwareUpdateFromBle(&carserver.VehicleData{SoftwareUpdateState: &carserver.SoftwareUpdateState{Status: tt.status}})
			if got.Status != tt.want {
				t.Errorf("status = %q, want %q", got.Status, tt.want)
			}
			if s := softwareUpdateStatus(tt.status); s != tt.want {
				t.Errorf("softwareUpdateStatus = %q, want %q", s, tt.want)
			}
		})
	}
	if got := softwareUpdateStatus(nil); got != "Unknown" {
		t.Errorf("softwareUpdateStatus(nil) = %q", got)
	}
}

// Each field maps to its own key; milliseconds are served as whole numbers, not in exponent form.
func TestSoftwareUpdateFieldMapping(t *testing.T) {
	s := &carserver.SoftwareUpdateState{
		OptionalScheduledTimeMs:        &carserver.SoftwareUpdateState_ScheduledTimeMs{ScheduledTimeMs: 1767258000000},
		OptionalWarningTimeRemainingMs: &carserver.SoftwareUpdateState_WarningTimeRemainingMs{WarningTimeRemainingMs: 600000},
		OptionalExpectedDurationSec:    &carserver.SoftwareUpdateState_ExpectedDurationSec{ExpectedDurationSec: 1500},
		OptionalDownloadPerc:           &carserver.SoftwareUpdateState_DownloadPerc{DownloadPerc: 41},
		OptionalInstallPerc:            &carserver.SoftwareUpdateState_InstallPerc{InstallPerc: 42},
		OptionalVersion:                &carserver.SoftwareUpdateState_Version{Version: "2026.32.5"},
		Timestamp:                      &timestamppb.Timestamp{Seconds: 1767254400},
	}
	want := `{"timestamp":1767254400,"status":"Unknown","scheduled_time_ms":1767258000000,"warning_time_remaining_ms":600000,` +
		`"expected_duration_sec":1500,"download_perc":41,"install_perc":42,"version":"2026.32.5"}`
	if got := softwareUpdateJSON(t, s); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestSoftwareUpdateZeroValues(t *testing.T) {
	for name, got := range map[string]string{
		"nil VehicleData":     mustJSON(t, SoftwareUpdateFromBle(nil)),
		"empty VehicleData":   mustJSON(t, SoftwareUpdateFromBle(&carserver.VehicleData{})),
		"empty state message": softwareUpdateJSON(t, &carserver.SoftwareUpdateState{}),
	} {
		if got != softwareUpdateZero {
			t.Errorf("%s: got %s\nwant %s", name, got, softwareUpdateZero)
		}
	}
	typ := reflect.TypeOf(SoftwareUpdate{})
	if typ.NumField() != 8 {
		t.Errorf("%d fields, want 8", typ.NumField())
	}
	for i := 0; i < typ.NumField(); i++ {
		if tag := typ.Field(i).Tag.Get("json"); tag == "" || strings.Contains(tag, "omitempty") {
			t.Errorf("field %s has json tag %q", typ.Field(i).Name, tag)
		}
	}
}

// A new field in the SDK message must be exposed: fail instead of dropping it silently.
func TestSoftwareUpdateCoversProtoFields(t *testing.T) {
	var keys map[string]any
	if err := json.Unmarshal([]byte(softwareUpdateZero), &keys); err != nil {
		t.Fatal(err)
	}
	fields := (&carserver.SoftwareUpdateState{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		if name := string(fields.Get(i).Name()); keys[name] == nil {
			t.Errorf("proto field %q of SoftwareUpdateState is not a key of software_update", name)
		}
	}
	if fields.Len() != len(keys) {
		t.Errorf("proto has %d fields, model has %d keys", fields.Len(), len(keys))
	}
}
