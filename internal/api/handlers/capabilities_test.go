package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

func getCapabilities(t *testing.T, routes ...string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc("/api/proxy/1/capabilities", Capabilities(func() []string { return routes })).Methods("GET")

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/1/capabilities", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestCapabilitiesEnvelope(t *testing.T) {
	previous := config.Version
	config.Version = "2.3.0-tb.1"
	t.Cleanup(func() { config.Version = previous })

	// No key in the working directory: key_role must be empty.
	t.Chdir(t.TempDir())

	rec := getCapabilities(t, "capabilities", "version")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type %q, want %q", contentType, "application/json")
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "" {
		t.Errorf("Cache-Control %q, want none", cacheControl)
	}

	want := `{"response":{"result":true,"reason":"The request was successfully processed.","vin":"","command":"capabilities","response":{` +
		`"api":1,"version":"2.3.0-tb.1","flavor":"superdcat",` +
		`"commands":` + mustJSON(t, commands.FleetCommandNames()) + `,` +
		`"vehicle_data_endpoints":` + mustJSON(t, commands.VehicleDataEndpointNames()) + `,` +
		`"proxy_routes":["capabilities","version"],` +
		`"features":{"strict_body_validation":true,"body_controller_state_queued":true,"auth_required":false},` +
		`"key_role":""}}}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestCapabilitiesWithoutBLE(t *testing.T) {
	t.Chdir(t.TempDir())

	previousInstance, previousEnqueue := control.BleControlInstance, enqueueCommand
	t.Cleanup(func() { control.BleControlInstance, enqueueCommand = previousInstance, previousEnqueue })
	control.BleControlInstance = nil
	enqueueCommand = func(context.Context, string, string, map[string]interface{}, *models.ApiResponse, bool) error {
		t.Errorf("capabilities must not queue any BLE command")
		return nil
	}

	start := time.Now()
	rec := getCapabilities(t, "capabilities")
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("answered in %v, want < 1s", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCapabilitiesAnnouncedCommandsAreRouted(t *testing.T) {
	for _, name := range commands.FleetCommandNames() {
		t.Run(name, func(t *testing.T) {
			stubQueue(t, func(response *models.ApiResponse) { response.Result = true })

			rec := postCommand(t, name, "", "")

			var ret models.Ret
			if err := json.Unmarshal(rec.Body.Bytes(), &ret); err != nil {
				t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
			}
			reason := ret.Response.Reason
			if strings.Contains(reason, "is not supported") {
				t.Fatalf("announced command %q rejected as unsupported: %q", name, reason)
			}
			switch {
			case rec.Code == http.StatusOK:
			case rec.Code == http.StatusServiceUnavailable && strings.HasPrefix(reason, "invalid request body: "):
			default:
				t.Errorf("status %d, reason %q", rec.Code, reason)
			}
		})
	}
}

func TestCapabilitiesAnnouncedEndpointsAreServed(t *testing.T) {
	previous := control.BleControlInstance
	t.Cleanup(func() { control.BleControlInstance = previous })
	control.BleControlInstance = nil

	router := mux.NewRouter()
	router.HandleFunc("/api/1/vehicles/{vin}/vehicle_data", VehicleData).Methods("GET")
	reasonFor := func(endpoint string) string {
		req := httptest.NewRequest(http.MethodGet, "/api/1/vehicles/"+testVIN+"/vehicle_data?endpoints="+endpoint, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var ret models.Ret
		if err := json.Unmarshal(rec.Body.Bytes(), &ret); err != nil {
			t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
		}
		return ret.Response.Reason
	}

	for _, endpoint := range commands.VehicleDataEndpointNames() {
		if got, want := reasonFor(endpoint), "BleControl is not initialized. Maybe private.pem is missing."; got != want {
			t.Errorf("endpoint %q: reason %q, want %q", endpoint, got, want)
		}
	}
	if got, want := reasonFor("nope"), `The endpoint "nope" is not supported.`; got != want {
		t.Errorf("endpoint nope: reason %q, want %q", got, want)
	}
}

func TestCapabilityFeaturesAuthRequired(t *testing.T) { // UC1007
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{"no configuration", nil, `"auth_required":false`},
		{"no token", &config.Config{}, `"auth_required":false`},
		{"blank token", &config.Config{APIToken: config.NewAPIToken("  ")}, `"auth_required":false`},
		{"token set", &config.Config{APIToken: config.NewAPIToken("secret")}, `"auth_required":true`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := config.AppConfig
			t.Cleanup(func() { config.AppConfig = previous })
			config.AppConfig = tt.cfg
			t.Chdir(t.TempDir())
			body := getCapabilities(t, "capabilities", "version").Body.String()
			if !strings.Contains(body, tt.want) {
				t.Errorf("body %s, want it to contain %s", body, tt.want)
			}
			if strings.Contains(body, "secret") {
				t.Errorf("token leaked in %s", body)
			}
		})
	}
}

// AC6: the endpoints announced to the clients are written out, not only read from the registry.
func TestCapabilitiesAnnounceVehicleDataEndpoints(t *testing.T) {
	t.Chdir(t.TempDir())

	var ret struct {
		Response struct {
			Response struct {
				Endpoints []string `json:"vehicle_data_endpoints"`
			} `json:"response"`
		} `json:"response"`
	}
	rec := getCapabilities(t, "capabilities")
	if err := json.Unmarshal(rec.Body.Bytes(), &ret); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	got := ret.Response.Response.Endpoints
	for _, want := range []string{"charge_state", "climate_state", "drive_state", "closures_state", "tire_pressure", "software_update", "location_data", "charge_schedule_data", "preconditioning_schedule_data"} {
		if !slices.Contains(got, want) {
			t.Errorf("vehicle_data_endpoints %v does not contain %q", got, want)
		}
	}
}
