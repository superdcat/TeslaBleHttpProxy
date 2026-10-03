package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
)

const testVIN = "TESLABLE000000001"

type queuedCommand struct {
	command string
	body    map[string]interface{}
	wait    bool
}

// stubQueue replaces the BLE queue for one test: commands are recorded and, with wait=true,
// outcome fills the response the BLE loop would have produced.
func stubQueue(t *testing.T, outcome func(*models.ApiResponse)) *[]queuedCommand {
	t.Helper()
	previousInstance, previousEnqueue := control.BleControlInstance, enqueueCommand
	t.Cleanup(func() { control.BleControlInstance, enqueueCommand = previousInstance, previousEnqueue })

	control.BleControlInstance = &control.BleControl{} // only checked for nil by the handler
	queued := &[]queuedCommand{}
	enqueueCommand = func(_ context.Context, command string, vin string, body map[string]interface{}, response *models.ApiResponse, autoWakeup bool) error {
		*queued = append(*queued, queuedCommand{command: command, body: body, wait: response != nil})
		if response != nil {
			outcome(response)
			response.Finish()
		}
		return nil
	}
	return queued
}

// postCommand serves POST /api/1/vehicles/<testVIN>/command/<command><query> with body ("" = no body).
func postCommand(t *testing.T, command string, query string, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc("/api/1/vehicles/{vin}/command/{command}", Command).Methods("POST")

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/1/vehicles/"+testVIN+"/command/"+command+query, reader)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// envelope is the exact 2.3.0 response body without inner response (json.Encoder adds the newline).
func envelope(result bool, reason string, command string) string {
	r := "false"
	if result {
		r = "true"
	}
	return `{"response":{"result":` + r + `,"reason":"` + reason + `","vin":"` + testVIN + `","command":"` + command + `"}}` + "\n"
}

func TestCommandRoute(t *testing.T) {
	const (
		missingAmps = "invalid request body: charging_amps missing"
		received    = "The command was successfully received and will be processed shortly."
		processed   = "The command was successfully processed."
		refused     = "failed to lock MESSAGEFAULT_ERROR_INSUFFICIENT_PRIVILEGES"
	)
	succeed := func(r *models.ApiResponse) { r.Result = true }
	refuse := func(r *models.ApiResponse) { r.Result = false; r.Error = refused }
	noEndpoints := func(r *models.ApiResponse) {
		r.Result = false
		r.Error = "missing or invalid 'endpoints' in request body"
	}

	tests := []struct {
		name    string
		command string
		query   string
		body    string
		outcome func(*models.ApiResponse)
		status  int
		want    string
		queued  []queuedCommand
	}{
		// AC1: refused before queuing, whatever wait is.
		{"empty object", "set_charging_amps", "", `{}`, nil, 503, envelope(false, missingAmps, "set_charging_amps"), nil},
		{"empty object wait=false", "set_charging_amps", "?wait=false", `{}`, nil, 503, envelope(false, missingAmps, "set_charging_amps"), nil},
		{"empty object wait=true", "set_charging_amps", "?wait=true", `{}`, nil, 503, envelope(false, missingAmps, "set_charging_amps"), nil},
		{"no body", "set_charging_amps", "", "", nil, 503, envelope(false, missingAmps, "set_charging_amps"), nil},
		{"malformed JSON", "set_charging_amps", "?wait=true", `{"charging_amps":16,}`, nil, 503,
			envelope(false, "invalid request body: not a valid JSON object", "set_charging_amps"), nil},
		{"not an object", "set_sentry_mode", "", `[true]`, nil, 503, envelope(false, "invalid request body: not a valid JSON object", "set_sentry_mode"), nil},
		{"sentry without on", "set_sentry_mode", "", `{"enable":true}`, nil, 503, envelope(false, "invalid request body: on missing", "set_sentry_mode"), nil},

		// Partial decoding (UnmarshalTypeError keeps the keys already read): never queued.
		{"partially decoded body refused", "set_sentry_mode", "", `{"on":true,"x":1e999}`, nil, 503,
			envelope(false, "invalid request body: not a valid JSON object", "set_sentry_mode"), nil},
		{"partially decoded body ignored without body", "flash_lights", "", `{"on":true,"x":1e999}`, nil, 200,
			envelope(true, received, "flash_lights"), []queuedCommand{{"flash_lights", nil, false}}},

		// AC2, AC5: 2.3.0 bodies still queued unchanged, without bounds.
		{"amps as string", "set_charging_amps", "", `{"charging_amps":"16"}`, nil, 200, envelope(true, received, "set_charging_amps"),
			[]queuedCommand{{"set_charging_amps", map[string]interface{}{"charging_amps": "16"}, false}}},
		{"amps as decimal", "set_charging_amps", "", `{"charging_amps":16.0}`, nil, 200, envelope(true, received, "set_charging_amps"),
			[]queuedCommand{{"set_charging_amps", map[string]interface{}{"charging_amps": 16.0}, false}}},
		{"amps 72", "set_charging_amps", "", `{"charging_amps":72}`, nil, 200, envelope(true, received, "set_charging_amps"),
			[]queuedCommand{{"set_charging_amps", map[string]interface{}{"charging_amps": 72.0}, false}}},
		{"sentry as string", "set_sentry_mode", "", `{"on":"true"}`, nil, 200, envelope(true, received, "set_sentry_mode"),
			[]queuedCommand{{"set_sentry_mode", map[string]interface{}{"on": "true"}, false}}},
		{"command without body ignores an unreadable body", "flash_lights", "", `not json`, nil, 200, envelope(true, received, "flash_lights"),
			[]queuedCommand{{"flash_lights", nil, false}}},

		// UC1010 AC2: climate setpoints refused before queuing, whatever wait is.
		{"temps empty object", "set_temps", "", `{}`, nil, 503,
			envelope(false, "invalid request body: driver_temp missing", "set_temps"), nil},
		{"temps empty object wait=true", "set_temps", "?wait=true", `{}`, nil, 503,
			envelope(false, "invalid request body: driver_temp missing", "set_temps"), nil},
		{"temps out of bounds", "set_temps", "", `{"driver_temp":28.1}`, nil, 503,
			envelope(false, "invalid request body: driver_temp must be between 15 and 28 degrees Celsius", "set_temps"), nil},
		{"temps as string queued unchanged", "set_temps", "", `{"driver_temp":"21"}`, nil, 200, envelope(true, received, "set_temps"),
			[]queuedCommand{{"set_temps", map[string]interface{}{"driver_temp": "21"}, false}}},
		{"preconditioning without on", "set_preconditioning_max", "", `{}`, nil, 503,
			envelope(false, "invalid request body: on missing", "set_preconditioning_max"), nil},
		{"preconditioning queued unchanged", "set_preconditioning_max", "", `{"on":true}`, nil, 200,
			envelope(true, received, "set_preconditioning_max"),
			[]queuedCommand{{"set_preconditioning_max", map[string]interface{}{"on": true}, false}}},

		// AC7: unchanged 2.3.0 answers.
		{"unsupported command", "x", "", `{}`, nil, 503, envelope(false, `The command \"x\" is not supported.`, "x"), nil},
		{"wait=true success", "set_charging_amps", "?wait=true", `{"charging_amps":16}`, succeed, 200, envelope(true, processed, "set_charging_amps"),
			[]queuedCommand{{"set_charging_amps", map[string]interface{}{"charging_amps": 16.0}, true}}},
		{"wait=true vehicle refusal", "door_lock", "?wait=true", "", refuse, 503, envelope(false, refused, "door_lock"),
			[]queuedCommand{{"door_lock", nil, true}}},
		// vehicle_data has no body check: it is queued, and the BLE loop answers with the
		// non-retryable error (see TestExecuteCommandReportsNonRetryableError).
		{"vehicle_data wait=true with body", "vehicle_data", "?wait=true", `{"endpoints":["charge_state"]}`, noEndpoints, 503,
			envelope(false, "missing or invalid 'endpoints' in request body", "vehicle_data"),
			[]queuedCommand{{"vehicle_data", map[string]interface{}{"endpoints": []interface{}{"charge_state"}}, true}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queued := stubQueue(t, tt.outcome)
			rec := postCommand(t, tt.command, tt.query, tt.body)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if got := rec.Body.String(); got != tt.want {
				t.Errorf("body = %s, want %s", got, tt.want)
			}
			if len(*queued) != len(tt.queued) || (len(tt.queued) > 0 && !reflect.DeepEqual(*queued, tt.queued)) {
				t.Errorf("queued = %+v, want %+v", *queued, tt.queued)
			}
		})
	}
}

// The missing key check still comes first, as in 2.3.0, even for an invalid body.
func TestCommandRouteWithoutKeyKeepsReason(t *testing.T) {
	queued := stubQueue(t, nil)
	control.BleControlInstance = nil

	rec := postCommand(t, "set_charging_amps", "", `{}`)

	want := envelope(false, "BleControl is not initialized. Maybe private.pem is missing.", "set_charging_amps")
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
		t.Errorf("got %d %s, want 503 %s", rec.Code, rec.Body.String(), want)
	}
	if len(*queued) != 0 {
		t.Errorf("queued = %+v, want nothing", *queued)
	}
}
