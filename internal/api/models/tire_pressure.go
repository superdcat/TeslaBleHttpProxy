package models

import (
	"math"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
)

// TirePressure contains the tire pressures, their last-seen times and the TPMS warnings.
// Written for the superdcat fork. Pressures are in bar, as the vehicle sends them (no
// conversion); times are Unix seconds. Every field is always present: a value the vehicle does
// not report is 0 or false, so a pressure of 0 means "not a reading".
type TirePressure struct {
	Timestamp                  int64   `json:"timestamp"`
	TpmsPressureFl             float32 `json:"tpms_pressure_fl"`
	TpmsPressureFr             float32 `json:"tpms_pressure_fr"`
	TpmsPressureRl             float32 `json:"tpms_pressure_rl"`
	TpmsPressureRr             float32 `json:"tpms_pressure_rr"`
	TpmsLastSeenPressureTimeFl int64   `json:"tpms_last_seen_pressure_time_fl"`
	TpmsLastSeenPressureTimeFr int64   `json:"tpms_last_seen_pressure_time_fr"`
	TpmsLastSeenPressureTimeRl int64   `json:"tpms_last_seen_pressure_time_rl"`
	TpmsLastSeenPressureTimeRr int64   `json:"tpms_last_seen_pressure_time_rr"`
	TpmsHardWarningFl          bool    `json:"tpms_hard_warning_fl"`
	TpmsHardWarningFr          bool    `json:"tpms_hard_warning_fr"`
	TpmsHardWarningRl          bool    `json:"tpms_hard_warning_rl"`
	TpmsHardWarningRr          bool    `json:"tpms_hard_warning_rr"`
	TpmsSoftWarningFl          bool    `json:"tpms_soft_warning_fl"`
	TpmsSoftWarningFr          bool    `json:"tpms_soft_warning_fr"`
	TpmsSoftWarningRl          bool    `json:"tpms_soft_warning_rl"`
	TpmsSoftWarningRr          bool    `json:"tpms_soft_warning_rr"`
	TpmsRcpFrontValue          float32 `json:"tpms_rcp_front_value"`
	TpmsRcpRearValue           float32 `json:"tpms_rcp_rear_value"`
}

// TirePressureFromBle converts the tire pressure state of the vehicle data; a nil argument or a
// missing tire pressure state gives the zero form.
func TirePressureFromBle(vehicleData *carserver.VehicleData) TirePressure {
	t := vehicleData.GetTirePressureState()
	return TirePressure{
		Timestamp:                  t.GetTimestamp().GetSeconds(),
		TpmsPressureFl:             finite32(t.GetTpmsPressureFl()),
		TpmsPressureFr:             finite32(t.GetTpmsPressureFr()),
		TpmsPressureRl:             finite32(t.GetTpmsPressureRl()),
		TpmsPressureRr:             finite32(t.GetTpmsPressureRr()),
		TpmsLastSeenPressureTimeFl: t.GetTpmsLastSeenPressureTimeFl().GetSeconds(),
		TpmsLastSeenPressureTimeFr: t.GetTpmsLastSeenPressureTimeFr().GetSeconds(),
		TpmsLastSeenPressureTimeRl: t.GetTpmsLastSeenPressureTimeRl().GetSeconds(),
		TpmsLastSeenPressureTimeRr: t.GetTpmsLastSeenPressureTimeRr().GetSeconds(),
		TpmsHardWarningFl:          t.GetTpmsHardWarningFl(),
		TpmsHardWarningFr:          t.GetTpmsHardWarningFr(),
		TpmsHardWarningRl:          t.GetTpmsHardWarningRl(),
		TpmsHardWarningRr:          t.GetTpmsHardWarningRr(),
		TpmsSoftWarningFl:          t.GetTpmsSoftWarningFl(),
		TpmsSoftWarningFr:          t.GetTpmsSoftWarningFr(),
		TpmsSoftWarningRl:          t.GetTpmsSoftWarningRl(),
		TpmsSoftWarningRr:          t.GetTpmsSoftWarningRr(),
		TpmsRcpFrontValue:          finite32(t.GetTpmsRcpFrontValue()),
		TpmsRcpRearValue:           finite32(t.GetTpmsRcpRearValue()),
	}
}

// finite32 maps NaN and +-Inf to 0: json.Marshal refuses them, which would fail the whole
// vehicle_data request. 0 already means "not a reading".
func finite32(v float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return 0
	}
	return v
}
