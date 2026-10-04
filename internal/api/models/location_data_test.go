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

const locationDataZero = `{"timestamp":0,"latitude":0,"longitude":0,"heading":0,"gps_as_of":0,"homelink_nearby":false,"location_name":""}`

// locationStateNotExposed lists the proto fields of LocationState that location_data does not serve.
var locationStateNotExposed = []string{
	"native_location_supported", "native_latitude", "native_longitude", "native_type",
	"corrected_latitude", "corrected_longitude",
	"geo_latitude", "geo_longitude", "geo_heading", "geo_elevation", "geo_accuracy",
	"estimated_gps_valid", "estimated_to_raw_distance",
}

func locationDataJSON(t *testing.T, s *carserver.LocationState) string {
	t.Helper()
	return mustJSON(t, LocationDataFromBle(&carserver.VehicleData{LocationState: s}))
}

// AC1: a hand-built message is converted with every expected key, in order.
func TestLocationDataJSON(t *testing.T) {
	s := &carserver.LocationState{
		OptionalLatitude:       &carserver.LocationState_Latitude{Latitude: 48.85837},
		OptionalLongitude:      &carserver.LocationState_Longitude{Longitude: 2.294481},
		OptionalHeading:        &carserver.LocationState_Heading{Heading: 271},
		OptionalGpsAsOf:        &carserver.LocationState_GpsAsOf{GpsAsOf: 1767254390},
		Timestamp:              &timestamppb.Timestamp{Seconds: 1767254400},
		OptionalHomelinkNearby: &carserver.LocationState_HomelinkNearby{HomelinkNearby: true},
		OptionalLocationName:   &carserver.LocationState_LocationName{LocationName: "Champ de Mars"},
	}
	want := `{"timestamp":1767254400,"latitude":48.85837,"longitude":2.294481,"heading":271,"gps_as_of":1767254390,` +
		`"homelink_nearby":true,"location_name":"Champ de Mars"}`
	if got := locationDataJSON(t, s); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Only the plain coordinates are served: the native, corrected and geo families never leak in.
func TestLocationDataUsesPlainCoordinates(t *testing.T) {
	s := &carserver.LocationState{
		OptionalNativeLatitude:     &carserver.LocationState_NativeLatitude{NativeLatitude: 11.5},
		OptionalNativeLongitude:    &carserver.LocationState_NativeLongitude{NativeLongitude: 12.5},
		OptionalCorrectedLatitude:  &carserver.LocationState_CorrectedLatitude{CorrectedLatitude: 13.5},
		OptionalCorrectedLongitude: &carserver.LocationState_CorrectedLongitude{CorrectedLongitude: 14.5},
		OptionalGeoLatitude:        &carserver.LocationState_GeoLatitude{GeoLatitude: 15.5},
		OptionalGeoLongitude:       &carserver.LocationState_GeoLongitude{GeoLongitude: 16.5},
		OptionalGeoHeading:         &carserver.LocationState_GeoHeading{GeoHeading: 17.5},
	}
	if got := locationDataJSON(t, s); got != locationDataZero {
		t.Errorf("got  %s\nwant %s", got, locationDataZero)
	}
}

// Explicit zero and false are on the wire: the keys are served, with those values.
func TestLocationDataExplicitZero(t *testing.T) {
	s := &carserver.LocationState{
		OptionalLatitude:       &carserver.LocationState_Latitude{},
		OptionalHeading:        &carserver.LocationState_Heading{},
		OptionalHomelinkNearby: &carserver.LocationState_HomelinkNearby{},
		OptionalLocationName:   &carserver.LocationState_LocationName{},
	}
	if got := locationDataJSON(t, s); got != locationDataZero {
		t.Errorf("got  %s\nwant %s", got, locationDataZero)
	}
}

// NaN and +-Inf coordinates are served as 0 (as in D-1017-03); the other fields are untouched.
func TestLocationDataNonFinite(t *testing.T) {
	for name, v := range map[string]float32{
		"NaN":  float32(math.NaN()),
		"+Inf": float32(math.Inf(1)),
		"-Inf": float32(math.Inf(-1)),
	} {
		t.Run(name, func(t *testing.T) {
			s := &carserver.LocationState{
				OptionalLatitude:  &carserver.LocationState_Latitude{Latitude: v},
				OptionalLongitude: &carserver.LocationState_Longitude{Longitude: v},
				OptionalHeading:   &carserver.LocationState_Heading{Heading: 90},
			}
			got := LocationDataFromBle(&carserver.VehicleData{LocationState: s})
			if got.Latitude != 0 || got.Longitude != 0 || got.Heading != 90 {
				t.Errorf("got %+v", got)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Errorf("json.Marshal: %v", err)
			}
		})
	}
}

func TestLocationDataZeroValues(t *testing.T) {
	for name, got := range map[string]string{
		"nil VehicleData":     mustJSON(t, LocationDataFromBle(nil)),
		"empty VehicleData":   mustJSON(t, LocationDataFromBle(&carserver.VehicleData{})),
		"empty state message": locationDataJSON(t, &carserver.LocationState{}),
	} {
		if got != locationDataZero {
			t.Errorf("%s: got %s\nwant %s", name, got, locationDataZero)
		}
	}
	typ := reflect.TypeOf(LocationData{})
	if typ.NumField() != 7 {
		t.Errorf("%d fields, want 7", typ.NumField())
	}
	for i := 0; i < typ.NumField(); i++ {
		if tag := typ.Field(i).Tag.Get("json"); tag == "" || strings.Contains(tag, "omitempty") {
			t.Errorf("field %s has json tag %q", typ.Field(i).Name, tag)
		}
	}
}

// A new field in the SDK message must be exposed or deliberately listed: fail instead of
// dropping it silently.
func TestLocationDataCoversProtoFields(t *testing.T) {
	var keys map[string]any
	if err := json.Unmarshal([]byte(locationDataZero), &keys); err != nil {
		t.Fatal(err)
	}
	for _, name := range locationStateNotExposed {
		if _, ok := keys[name]; ok {
			t.Errorf("%q is listed as not exposed but is a key of location_data", name)
		}
	}
	fields := (&carserver.LocationState{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		name := string(fields.Get(i).Name())
		_, isKey := keys[name]
		if !isKey && !slices.Contains(locationStateNotExposed, name) {
			t.Errorf("proto field %q of LocationState is neither a key of location_data nor listed as not exposed", name)
		}
	}
	if len(keys)+len(locationStateNotExposed) != fields.Len() {
		t.Errorf("%d keys + %d not exposed, proto has %d fields", len(keys), len(locationStateNotExposed), fields.Len())
	}
}
