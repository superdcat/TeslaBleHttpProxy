package models

import (
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
)

// LocationData contains the position of the vehicle: a subset of the LocationState message.
// Written for the superdcat fork. This is personal data: it must never be logged (UC1018 AC4).
// latitude and longitude are the plain coordinates (WGS-84, degrees); the native, corrected
// (China) and geo coordinate families of the message are not exposed. Every field is always
// present: a value the vehicle does not report is 0, false or "", so gps_as_of 0 together with
// a latitude and a longitude of 0 means "no position".
type LocationData struct {
	Timestamp int64   `json:"timestamp"`
	Latitude  float32 `json:"latitude"`
	Longitude float32 `json:"longitude"`
	Heading   uint32  `json:"heading"`
	// GpsAsOf is served as the vehicle sends it (Unix seconds expected, the unit is not
	// documented in the protocol definitions).
	GpsAsOf uint64 `json:"gps_as_of"`
	// HomelinkNearby is only set by vehicles supporting HomeLink; false also means "not supported".
	HomelinkNearby bool `json:"homelink_nearby"`
	// LocationName is a non-precise location name, possibly empty.
	LocationName string `json:"location_name"`
}

// LocationDataFromBle converts the location state of the vehicle data; a nil argument or a
// missing location state gives the zero form.
func LocationDataFromBle(vehicleData *carserver.VehicleData) LocationData {
	l := vehicleData.GetLocationState()
	return LocationData{
		Timestamp:      l.GetTimestamp().GetSeconds(),
		Latitude:       finite32(l.GetLatitude()),
		Longitude:      finite32(l.GetLongitude()),
		Heading:        l.GetHeading(),
		GpsAsOf:        l.GetGpsAsOf(),
		HomelinkNearby: l.GetHomelinkNearby(),
		LocationName:   l.GetLocationName(),
	}
}
