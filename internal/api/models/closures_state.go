package models

import (
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
)

// ClosuresState contains the doors, windows, sunroof, lock and sentry states of the vehicle.
// Written for the superdcat fork. Every field is always present: a boolean or number the vehicle
// does not report is false or 0, an enum it does not report is "<nil>".
type ClosuresState struct {
	Timestamp                int64  `json:"timestamp"`
	DoorOpenDriverFront      bool   `json:"door_open_driver_front"`
	DoorOpenDriverRear       bool   `json:"door_open_driver_rear"`
	DoorOpenPassengerFront   bool   `json:"door_open_passenger_front"`
	DoorOpenPassengerRear    bool   `json:"door_open_passenger_rear"`
	DoorOpenTrunkFront       bool   `json:"door_open_trunk_front"`
	DoorOpenTrunkRear        bool   `json:"door_open_trunk_rear"`
	WindowOpenDriverFront    bool   `json:"window_open_driver_front"`
	WindowOpenPassengerFront bool   `json:"window_open_passenger_front"`
	WindowOpenDriverRear     bool   `json:"window_open_driver_rear"`
	WindowOpenPassengerRear  bool   `json:"window_open_passenger_rear"`
	SunRoofState             string `json:"sun_roof_state"`
	SunRoofPercentOpen       int32  `json:"sun_roof_percent_open"`
	Locked                   bool   `json:"locked"`
	IsUserPresent            bool   `json:"is_user_present"`
	ValetMode                bool   `json:"valet_mode"`
	SentryModeState          string `json:"sentry_mode_state"`
	SentryMode               bool   `json:"sentry_mode"`
	TonneauState             string `json:"tonneau_state"`
	TonneauPercentOpen       uint32 `json:"tonneau_percent_open"`
	TonneauInMotion          bool   `json:"tonneau_in_motion"`
}

// ClosuresStateFromBle converts the closures state of the vehicle data; a nil argument or a
// missing closures state gives the zero form.
func ClosuresStateFromBle(vehicleData *carserver.VehicleData) ClosuresState {
	c := vehicleData.GetClosuresState()
	tonneau := "<nil>"
	if c.GetOptionalTonneauState() != nil {
		tonneau = flatten(c.GetTonneauState().String())
	}
	return ClosuresState{
		Timestamp:                c.GetTimestamp().GetSeconds(),
		DoorOpenDriverFront:      c.GetDoorOpenDriverFront(),
		DoorOpenDriverRear:       c.GetDoorOpenDriverRear(),
		DoorOpenPassengerFront:   c.GetDoorOpenPassengerFront(),
		DoorOpenPassengerRear:    c.GetDoorOpenPassengerRear(),
		DoorOpenTrunkFront:       c.GetDoorOpenTrunkFront(),
		DoorOpenTrunkRear:        c.GetDoorOpenTrunkRear(),
		WindowOpenDriverFront:    c.GetWindowOpenDriverFront(),
		WindowOpenPassengerFront: c.GetWindowOpenPassengerFront(),
		WindowOpenDriverRear:     c.GetWindowOpenDriverRear(),
		WindowOpenPassengerRear:  c.GetWindowOpenPassengerRear(),
		SunRoofState:             flatten(c.GetSunRoofState().String()),
		SunRoofPercentOpen:       c.GetSunRoofPercentOpen(),
		Locked:                   c.GetLocked(),
		IsUserPresent:            c.GetIsUserPresent(),
		ValetMode:                c.GetValetMode(),
		SentryModeState:          flatten(c.GetSentryModeState().String()),
		SentryMode:               sentryModeOn(c.GetSentryModeState()),
		TonneauState:             tonneau,
		TonneauPercentOpen:       c.GetTonneauPercentOpen(),
		TonneauInMotion:          c.GetTonneauInMotion(),
	}
}

// sentryModeOn derives the sentry_mode boolean (UC1016 AC2): Armed, Aware, Panic and Quiet give
// true; Off, Idle, an empty message and a missing state give false. It does not depend on String().
func sentryModeOn(state *carserver.ClosuresState_SentryModeState) bool {
	switch state.GetType().(type) {
	case *carserver.ClosuresState_SentryModeState_Armed,
		*carserver.ClosuresState_SentryModeState_Aware,
		*carserver.ClosuresState_SentryModeState_Panic,
		*carserver.ClosuresState_SentryModeState_Quiet:
		return true
	}
	return false
}
