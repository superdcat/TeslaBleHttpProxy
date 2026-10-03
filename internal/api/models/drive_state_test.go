package models

import (
	"encoding/json"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func driveJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDriveStateJSON(t *testing.T) {
	full := &carserver.DriveState{
		ShiftState:                          &carserver.ShiftState{Type: &carserver.ShiftState_D{D: &carserver.Void{}}},
		OptionalSpeedFloat:                  &carserver.DriveState_SpeedFloat{SpeedFloat: 12.5},
		OptionalSpeed:                       &carserver.DriveState_Speed{Speed: 13},
		OptionalPower:                       &carserver.DriveState_Power{Power: 42},
		Timestamp:                           &timestamppb.Timestamp{Seconds: 1767254400},
		OptionalOdometerInHundredthsOfAMile: &carserver.DriveState_OdometerInHundredthsOfAMile{OdometerInHundredthsOfAMile: 1234567},
	}

	tests := []struct {
		name string
		data *carserver.DriveState
		want string
	}{
		{"full", full, `{"timestamp":1767254400,"shift_state":"D","speed":12.5,"power":42,"odometer":12345.67}`},
		{"integer speed only", &carserver.DriveState{OptionalSpeed: &carserver.DriveState_Speed{Speed: 37}}, `{"timestamp":0,"shift_state":"","speed":37,"power":0,"odometer":0}`},
		{"speed_float wins", &carserver.DriveState{OptionalSpeed: &carserver.DriveState_Speed{Speed: 13}, OptionalSpeedFloat: &carserver.DriveState_SpeedFloat{SpeedFloat: 12.5}}, `{"timestamp":0,"shift_state":"","speed":12.5,"power":0,"odometer":0}`},
		{"negative power", &carserver.DriveState{OptionalPower: &carserver.DriveState_Power{Power: -15}}, `{"timestamp":0,"shift_state":"","speed":0,"power":-15,"odometer":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := driveJSON(t, DriveStateFromBle(&carserver.VehicleData{DriveState: tt.data}))
			if got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestDriveStateJSONZeroValues(t *testing.T) {
	const zero = `{"timestamp":0,"shift_state":"","speed":0,"power":0,"odometer":0}`
	if got := driveJSON(t, DriveStateFromBle(&carserver.VehicleData{})); got != zero {
		t.Errorf("empty VehicleData: got %s, want %s", got, zero)
	}
	if got := driveJSON(t, DriveState{}); got != zero {
		t.Errorf("zero DriveState: got %s, want %s", got, zero)
	}
	for name, shift := range map[string]*carserver.ShiftState{
		"SNA":     {Type: &carserver.ShiftState_SNA{SNA: &carserver.Void{}}},
		"Invalid": {Type: &carserver.ShiftState_Invalid{Invalid: &carserver.Void{}}},
	} {
		got := driveJSON(t, DriveStateFromBle(&carserver.VehicleData{DriveState: &carserver.DriveState{ShiftState: shift}}))
		if got != zero {
			t.Errorf("%s: got %s, want %s", name, got, zero)
		}
	}
}
