package models

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

// testdata/*.binpb and testdata/*.golden.json were written once with -update on the previous
// SDK and must not be regenerated to make a test pass. The .binpb files pin the wire encoding
// (field numbers, as the vehicle sends it over BLE); the .golden.json files pin the JSON served
// by vehicle_data and body_controller_state (field names, order, enum strings, "<nil>").
var update = flag.Bool("update", false, "rewrite testdata/*.binpb and testdata/*.golden.json (previous SDK only)")

func readTextProto(t *testing.T, name string, m proto.Message) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := prototext.Unmarshal(b, m); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// readWireInput decodes testdata/<name>.binpb into m and checks that it still equals the
// message described by testdata/<name>.txtpb. Text format addresses fields by name, the wire
// format by number: a field renumbered in the SDK protobuf definitions makes them differ.
func readWireInput(t *testing.T, name string, m proto.Message) {
	t.Helper()
	text := m.ProtoReflect().New().Interface()
	readTextProto(t, name+".txtpb", text)
	path := filepath.Join("testdata", name+".binpb")
	if *update {
		b, err := proto.MarshalOptions{Deterministic: true}.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(b, m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if !proto.Equal(m, text) {
		t.Fatalf("%s no longer decodes to %s.txtpb: a field number changed in the SDK protobuf definitions", path, name)
	}
}

func TestStatesMatchGolden(t *testing.T) {
	var vd carserver.VehicleData
	readWireInput(t, "vehicle_data", &vd)
	var awake, asleep, open vcsec.VehicleStatus
	readWireInput(t, "vehicle_status_awake", &awake)
	readWireInput(t, "vehicle_status_asleep", &asleep)
	readWireInput(t, "vehicle_status_open", &open)

	tests := []struct {
		golden string
		value  any
	}{
		{"charge_state.golden.json", ChargeStateFromBle(&vd)},
		{"climate_state.golden.json", ClimateStateFromBle(&vd)},
		{"body_controller_state_awake.golden.json", VehicleStatusFromBle(&awake)},
		{"body_controller_state_asleep.golden.json", VehicleStatusFromBle(&asleep)},
		{"body_controller_state_open.golden.json", VehicleStatusFromBle(&open)},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) { checkGolden(t, tt.golden, tt.value) })
	}
}

// checkGolden compares the indented JSON of value with testdata/<golden> (rewritten with -update).
func checkGolden(t *testing.T, golden string, value any) {
	t.Helper()
	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", golden)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the converter output:\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// TestDriveStateMatchesGolden pins the wire encoding and the JSON of drive_state (wimaha PR #160 contract).
func TestDriveStateMatchesGolden(t *testing.T) {
	var vd carserver.VehicleData
	readWireInput(t, "drive_state", &vd)
	checkGolden(t, "drive_state.golden.json", DriveStateFromBle(&vd))
}

// TestClosuresStateMatchesGolden pins the wire encoding and the JSON of closures_state (UC1016).
func TestClosuresStateMatchesGolden(t *testing.T) {
	var vd carserver.VehicleData
	readWireInput(t, "closures_state", &vd)
	checkGolden(t, "closures_state.golden.json", ClosuresStateFromBle(&vd))
}

// TestTirePressureMatchesGolden pins the wire encoding and the JSON of tire_pressure (UC1017).
func TestTirePressureMatchesGolden(t *testing.T) {
	var vd carserver.VehicleData
	readWireInput(t, "tire_pressure", &vd)
	checkGolden(t, "tire_pressure.golden.json", TirePressureFromBle(&vd))
}

// TestSoftwareUpdateMatchesGolden pins the wire encoding and the JSON of software_update (UC1017).
func TestSoftwareUpdateMatchesGolden(t *testing.T) {
	var vd carserver.VehicleData
	readWireInput(t, "software_update", &vd)
	checkGolden(t, "software_update.golden.json", SoftwareUpdateFromBle(&vd))
}
