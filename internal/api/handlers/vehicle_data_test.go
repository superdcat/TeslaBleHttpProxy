package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	charmlog "github.com/charmbracelet/log"
	"github.com/gorilla/mux"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
	"google.golang.org/protobuf/proto"
)

const (
	vehicleDataProcessed = "The request was successfully processed."
	driveStateJSON       = `{"timestamp":1767254400,"shift_state":"D","speed":12.5,"power":42,"odometer":12345.67}`
)

// resetVehicleDataCache empties the global vehicle_data cache.
func resetVehicleDataCache() {
	vehicleDataCacheMux.Lock()
	defer vehicleDataCacheMux.Unlock()
	for key := range vehicleDataCache {
		delete(vehicleDataCache, key)
	}
}

// useVehicleDataCache sets the cache duration and empties the global cache, before and after the test.
func useVehicleDataCache(t *testing.T, seconds int) {
	t.Helper()
	previous := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = previous
		resetVehicleDataCache()
	})
	config.AppConfig = &config.Config{VehicleDataCacheTime: seconds}
	resetVehicleDataCache()
}

type queuedVehicleData struct {
	command    string
	endpoints  []string
	autoWakeup bool
}

// vehicleDataQueue replaces the BLE queue: it notes each queued read and answers like the BLE loop,
// from the wire fixtures of the models golden tests (drive_state.binpb for the drive category,
// closures_state.binpb for the closures category, tire_pressure.binpb, software_update.binpb and location_data.binpb, charge_schedule_data.binpb and preconditioning_schedule_data.binpb for theirs, vehicle_data.binpb for the others).
func vehicleDataQueue(t *testing.T) *[]queuedVehicleData {
	t.Helper()
	queued := &[]queuedVehicleData{}
	useQueue(t, func(ctx context.Context, command string, vin string, body map[string]interface{}, response *models.ApiResponse, autoWakeup bool) error {
		endpoints, _ := body["endpoints"].([]string)
		*queued = append(*queued, queuedVehicleData{command, endpoints, autoWakeup})
		read := func(_ context.Context, category vehicle.StateCategory) (*carserver.VehicleData, error) {
			name, ok := map[vehicle.StateCategory]string{
				vehicle.StateCategoryDrive:                   "drive_state",
				vehicle.StateCategoryClosures:                "closures_state",
				vehicle.StateCategoryTirePressure:            "tire_pressure",
				vehicle.StateCategorySoftwareUpdate:          "software_update",
				vehicle.StateCategoryLocation:                "location_data",
				vehicle.StateCategoryChargeSchedule:          "charge_schedule_data",
				vehicle.StateCategoryPreconditioningSchedule: "preconditioning_schedule_data",
			}[category]
			if !ok {
				name = "vehicle_data"
			}
			b, err := os.ReadFile(filepath.Join("..", "models", "testdata", name+".binpb"))
			if err != nil {
				return nil, err
			}
			var vd carserver.VehicleData
			if err := proto.Unmarshal(b, &vd); err != nil {
				return nil, err
			}
			return &vd, nil
		}
		j, _, err := commands.VehicleDataJSON(ctx, endpoints, read)
		if err != nil {
			response.Error = err.Error()
		} else {
			response.Result, response.Response = true, j
		}
		response.Finish()
		return nil
	})
	return queued
}

func getVehicleData(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc("/api/1/vehicles/{vin}/vehicle_data", VehicleData).Methods("GET")
	req := httptest.NewRequest(http.MethodGet, "/api/1/vehicles/"+testVIN+"/vehicle_data"+query, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func vehicleDataBody(inner string) string {
	return `{"response":{"result":true,"reason":"` + vehicleDataProcessed + `","vin":"` + testVIN +
		`","command":"vehicle_data","response":` + inner + `}}` + "\n"
}

func compactGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "models", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// AC3: drive_state is served in the standard envelope.
func TestVehicleDataDriveState(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)

	rec := getVehicleData(t, "?endpoints=drive_state")

	want := vehicleDataBody(`{"drive_state":` + driveStateJSON + `}`)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
	}
	if wantQueued := []queuedVehicleData{{"vehicle_data", []string{"drive_state"}, false}}; !reflect.DeepEqual(*queued, wantQueued) {
		t.Errorf("queued %+v, want %+v", *queued, wantQueued)
	}
}

// UC1016 AC4: closures_state is served in the standard envelope, alone and combined; the wake-up
// stays opt-in.
func TestVehicleDataClosuresState(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)
	closures := compactGolden(t, "closures_state.golden.json")

	rec := getVehicleData(t, "?endpoints=closures_state")
	want := vehicleDataBody(`{"closures_state":` + closures + `}`)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
	}
	if wantQueued := []queuedVehicleData{{"vehicle_data", []string{"closures_state"}, false}}; !reflect.DeepEqual(*queued, wantQueued) {
		t.Errorf("queued %+v, want %+v", *queued, wantQueued)
	}

	resetVehicleDataCache()
	if rec := getVehicleData(t, "?endpoints=closures_state&wakeup=true"); rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("wakeup: got %d %s", rec.Code, rec.Body.String())
	}
	if last := (*queued)[len(*queued)-1]; !last.autoWakeup {
		t.Errorf("last queued read %+v, want autoWakeup", last)
	}
}

func TestVehicleDataClosuresStateWithCharge(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)

	rec := getVehicleData(t, "?endpoints="+url.QueryEscape("charge_state;closures_state"))

	want := vehicleDataBody(`{"charge_state":` + compactGolden(t, "charge_state.golden.json") +
		`,"closures_state":` + compactGolden(t, "closures_state.golden.json") + `}`)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
	}
	if wantQueued := []queuedVehicleData{{"vehicle_data", []string{"charge_state", "closures_state"}, false}}; !reflect.DeepEqual(*queued, wantQueued) {
		t.Errorf("queued %+v, want %+v", *queued, wantQueued)
	}
}

// AC4, AC7: the default is still charge_state and climate_state, as served by 2.3.0 (golden files).
func TestVehicleDataDefaultMatches230(t *testing.T) {
	for _, query := range []string{"", "?endpoints="} {
		t.Run("query "+query, func(t *testing.T) {
			useVehicleDataCache(t, 30)
			queued := vehicleDataQueue(t)

			rec := getVehicleData(t, query)

			want := vehicleDataBody(`{"charge_state":` + compactGolden(t, "charge_state.golden.json") +
				`,"climate_state":` + compactGolden(t, "climate_state.golden.json") + `}`)
			if rec.Code != http.StatusOK || rec.Body.String() != want {
				t.Errorf("got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
			}
			if wantQueued := []queuedVehicleData{{"vehicle_data", []string{"charge_state", "climate_state"}, false}}; !reflect.DeepEqual(*queued, wantQueued) {
				t.Errorf("queued %+v, want %+v", *queued, wantQueued)
			}
		})
	}
}

// AC4: endpoints combine.
func TestVehicleDataCombinedEndpoints(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)

	rec := getVehicleData(t, "?endpoints="+url.QueryEscape("charge_state;drive_state"))

	want := vehicleDataBody(`{"charge_state":` + compactGolden(t, "charge_state.golden.json") + `,"drive_state":` + driveStateJSON + `}`)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
	}
	if wantQueued := []queuedVehicleData{{"vehicle_data", []string{"charge_state", "drive_state"}, false}}; !reflect.DeepEqual(*queued, wantQueued) {
		t.Errorf("queued %+v, want %+v", *queued, wantQueued)
	}
}

// AC5: an unknown endpoint fails the whole request before anything is queued (exact, case-sensitive match).
func TestVehicleDataUnsupportedEndpoint(t *testing.T) {
	for _, tc := range []struct{ query, bad string }{
		{"x", "x"},
		{"nimportequoi", "nimportequoi"},
		{"drive", "drive"},
		{"Drive_State", "Drive_State"},
		{"DRIVE_STATE", "DRIVE_STATE"},
		{"charge-schedule", "charge-schedule"},
		{"tire-pressure", "tire-pressure"},
		{"software-update", "software-update"},
		{"tire", "tire"},
		{"Tire_Pressure", "Tire_Pressure"},
		{"tire_pressure_state", "tire_pressure_state"},
		{"Software_Update", "Software_Update"},
		{"software_update_state", "software_update_state"},
		{"tire_pressure;", ""},
		{"software_update;", ""},
		{"location", "location"},
		{"Location_Data", "Location_Data"},
		{"location_state", "location_state"},
		{"location-data", "location-data"},
		{"location_data;", ""},
		{"charge_schedule", "charge_schedule"},
		{"Charge_Schedule_Data", "Charge_Schedule_Data"},
		{"charge_schedule_state", "charge_schedule_state"},
		{"charge-schedule-data", "charge-schedule-data"},
		{"charge_schedule_data;", ""},
		{"precondition-schedule", "precondition-schedule"},
		{"preconditioning_schedule", "preconditioning_schedule"},
		{"precondition_schedule_data", "precondition_schedule_data"},
		{"Preconditioning_Schedule_Data", "Preconditioning_Schedule_Data"},
		{"preconditioning_schedule_state", "preconditioning_schedule_state"},
		{"preconditioning_schedule_data;", ""},
		{"charge_state;nimportequoi", "nimportequoi"},
		{"drive_state;", ""},
		{"closures", "closures"},
		{"Closures_State", "Closures_State"},
		{"closure_state", "closure_state"},
		{"closures-state", "closures-state"},
		{"closures_state;", ""},
	} {
		t.Run(tc.query, func(t *testing.T) {
			useVehicleDataCache(t, 30)
			queued := vehicleDataQueue(t)

			rec := getVehicleData(t, "?endpoints="+url.QueryEscape(tc.query))

			want := envelope(false, `The endpoint \"`+tc.bad+`\" is not supported.`, "vehicle_data")
			if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
				t.Errorf("got %d %s\nwant 503 %s", rec.Code, rec.Body.String(), want)
			}
			if len(*queued) != 0 {
				t.Errorf("queued %+v, want nothing", *queued)
			}
		})
	}
}

// The endpoint check comes before checkBleControl: no BLE instance still gives the 503 "not supported".
func TestVehicleDataUnsupportedEndpointWithoutBle(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)
	previous := control.BleControlInstance
	t.Cleanup(func() { control.BleControlInstance = previous })
	control.BleControlInstance = nil

	rec := getVehicleData(t, "?endpoints=x")

	want := envelope(false, `The endpoint \"x\" is not supported.`, "vehicle_data")
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
		t.Errorf("got %d %s\nwant 503 %s", rec.Code, rec.Body.String(), want)
	}
	if len(*queued) != 0 {
		t.Errorf("queued %+v, want nothing", *queued)
	}
}

// The 30 s cache is per VIN and endpoint; wakeup=true is the only way to ask for a wake-up.
func TestVehicleDataDriveStateCacheAndWakeup(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)
	wantBody := vehicleDataBody(`{"drive_state":` + driveStateJSON + `}`)

	for i := 0; i < 2; i++ {
		if rec := getVehicleData(t, "?endpoints=drive_state"); rec.Code != http.StatusOK || rec.Body.String() != wantBody {
			t.Fatalf("call %d: got %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if len(*queued) != 1 {
		t.Fatalf("queued %d reads for two requests within the cache time, want 1", len(*queued))
	}

	// drive_state is cached, charge_state is not: as in 2.3.0 the handler queues every endpoint of the request as soon as one is missing.
	rec := getVehicleData(t, "?endpoints="+url.QueryEscape("drive_state;charge_state"))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if len(*queued) != 2 || !slices.Equal((*queued)[1].endpoints, []string{"drive_state", "charge_state"}) {
		t.Errorf("queued %+v", *queued)
	}

	resetVehicleDataCache()
	if rec := getVehicleData(t, "?endpoints=drive_state&wakeup=true"); rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if last := (*queued)[len(*queued)-1]; !last.autoWakeup {
		t.Errorf("last queued read %+v, want autoWakeup", last)
	}
}

// UC1017 AC3: tire_pressure and software_update are served in the standard envelope, alone and
// combined (the semicolon is encoded: see TestVehicleDataRawSemicolonServesDefault); the wake-up
// stays opt-in.
func TestVehicleDataTirePressureSoftwareUpdate(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)
	tires := compactGolden(t, "tire_pressure.golden.json")
	update := compactGolden(t, "software_update.golden.json")

	for _, tc := range []struct {
		name, query, body string
		endpoints         []string
	}{
		{"tire_pressure", "tire_pressure", `{"tire_pressure":` + tires + `}`, []string{"tire_pressure"}},
		{"software_update", "software_update", `{"software_update":` + update + `}`, []string{"software_update"}},
		{"both", url.QueryEscape("tire_pressure;software_update"), `{"software_update":` + update + `,"tire_pressure":` + tires + `}`,
			[]string{"tire_pressure", "software_update"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetVehicleDataCache()
			*queued = nil
			rec := getVehicleData(t, "?endpoints="+tc.query)
			if want := vehicleDataBody(tc.body); rec.Code != http.StatusOK || rec.Body.String() != want {
				t.Errorf("got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
			}
			if wantQueued := []queuedVehicleData{{"vehicle_data", tc.endpoints, false}}; !reflect.DeepEqual(*queued, wantQueued) {
				t.Errorf("queued %+v, want %+v", *queued, wantQueued)
			}
		})
	}

	resetVehicleDataCache()
	if rec := getVehicleData(t, "?endpoints=tire_pressure&wakeup=true"); rec.Code != http.StatusOK {
		t.Fatalf("wakeup: got %d %s", rec.Code, rec.Body.String())
	}
	if last := (*queued)[len(*queued)-1]; !last.autoWakeup {
		t.Errorf("last queued read %+v, want autoWakeup", last)
	}
}

// A category the vehicle refuses fails the whole request, like any other read failure.
func TestVehicleDataTirePressureRefused(t *testing.T) {
	useVehicleDataCache(t, 30)
	useQueue(t, func(ctx context.Context, command string, vin string, body map[string]interface{}, response *models.ApiResponse, autoWakeup bool) error {
		endpoints, ok := body["endpoints"].([]string)
		if !ok {
			t.Errorf("body[endpoints] is %T, want []string", body["endpoints"])
		} else if want := []string{"tire_pressure", "software_update"}; !reflect.DeepEqual(endpoints, want) {
			t.Errorf("endpoints %v, want %v", endpoints, want)
		}
		_, _, err := commands.VehicleDataJSON(ctx, endpoints, func(context.Context, vehicle.StateCategory) (*carserver.VehicleData, error) {
			return nil, errors.New("category not supported")
		})
		if err == nil {
			t.Error("VehicleDataJSON returned no error")
			err = errors.New("no error")
		}
		response.Error = err.Error()
		response.Finish()
		return nil
	})

	rec := getVehicleData(t, "?endpoints="+url.QueryEscape("tire_pressure;software_update"))

	want := envelope(false, "Failed to get vehicle data: category not supported", "vehicle_data")
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
		t.Errorf("got %d %s\nwant 503 %s", rec.Code, rec.Body.String(), want)
	}
}

// Known limitation inherited from 2.3.0; invert when the parsing is fixed. Since Go 1.17 a raw
// semicolon makes url.ParseQuery drop the whole parameter, so the default endpoints are served
// without any error: clients must encode it as %3B (UC1017 D-1017-04).
func TestVehicleDataRawSemicolonServesDefault(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)

	rec := getVehicleData(t, "?endpoints=tire_pressure;software_update")

	want := vehicleDataBody(`{"charge_state":` + compactGolden(t, "charge_state.golden.json") +
		`,"climate_state":` + compactGolden(t, "climate_state.golden.json") + `}`)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
	}
	if wantQueued := []queuedVehicleData{{"vehicle_data", []string{"charge_state", "climate_state"}, false}}; !reflect.DeepEqual(*queued, wantQueued) {
		t.Errorf("queued %+v, want %+v", *queued, wantQueued)
	}
}

// UC1018 AC2: location_data is served in the standard envelope, alone and combined (the
// semicolon is encoded); the wake-up stays opt-in.
func TestVehicleDataLocationData(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)
	location := compactGolden(t, "location_data.golden.json")

	rec := getVehicleData(t, "?endpoints=location_data")
	want := vehicleDataBody(`{"location_data":` + location + `}`)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
	}
	if wantQueued := []queuedVehicleData{{"vehicle_data", []string{"location_data"}, false}}; !reflect.DeepEqual(*queued, wantQueued) {
		t.Errorf("queued %+v, want %+v", *queued, wantQueued)
	}

	resetVehicleDataCache()
	if rec := getVehicleData(t, "?endpoints=location_data&wakeup=true"); rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("wakeup: got %d %s", rec.Code, rec.Body.String())
	}
	if last := (*queued)[len(*queued)-1]; !last.autoWakeup {
		t.Errorf("last queued read %+v, want autoWakeup", last)
	}

	resetVehicleDataCache()
	*queued = nil
	rec = getVehicleData(t, "?endpoints="+url.QueryEscape("charge_state;location_data"))
	want = vehicleDataBody(`{"charge_state":` + compactGolden(t, "charge_state.golden.json") + `,"location_data":` + location + `}`)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("combined: got %d %s\nwant 200 %s", rec.Code, rec.Body.String(), want)
	}
	if wantQueued := []queuedVehicleData{{"vehicle_data", []string{"charge_state", "location_data"}, false}}; !reflect.DeepEqual(*queued, wantQueued) {
		t.Errorf("queued %+v, want %+v", *queued, wantQueued)
	}
}

// UC1019 AC3: charge_schedule_data and preconditioning_schedule_data are served in the standard
// envelope, alone and combined (the semicolon is encoded); the wake-up stays opt-in.
func TestVehicleDataScheduleData(t *testing.T) {
	useVehicleDataCache(t, 30)
	queued := vehicleDataQueue(t)
	charge := compactGolden(t, "charge_schedule_data.golden.json")
	precondition := compactGolden(t, "preconditioning_schedule_data.golden.json")

	for _, tc := range []struct {
		query     string
		endpoints []string
		body      string
	}{
		{"charge_schedule_data", []string{"charge_schedule_data"}, `{"charge_schedule_data":` + charge + `}`},
		{"preconditioning_schedule_data", []string{"preconditioning_schedule_data"}, `{"preconditioning_schedule_data":` + precondition + `}`},
		{"charge_schedule_data;preconditioning_schedule_data", []string{"charge_schedule_data", "preconditioning_schedule_data"},
			`{"charge_schedule_data":` + charge + `,"preconditioning_schedule_data":` + precondition + `}`},
	} {
		resetVehicleDataCache()
		*queued = nil
		rec := getVehicleData(t, "?endpoints="+url.QueryEscape(tc.query))
		if want := vehicleDataBody(tc.body); rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s: got %d %s\nwant 200 %s", tc.query, rec.Code, rec.Body.String(), want)
		}
		if wantQueued := []queuedVehicleData{{"vehicle_data", tc.endpoints, false}}; !reflect.DeepEqual(*queued, wantQueued) {
			t.Errorf("%s: queued %+v, want %+v", tc.query, *queued, wantQueued)
		}
	}

	resetVehicleDataCache()
	if rec := getVehicleData(t, "?endpoints=charge_schedule_data&wakeup=true"); rec.Code != http.StatusOK {
		t.Fatalf("wakeup: got %d %s", rec.Code, rec.Body.String())
	}
	if last := (*queued)[len(*queued)-1]; !last.autoWakeup {
		t.Errorf("last queued read %+v, want autoWakeup", last)
	}
}

// UC1019 AC2 over HTTP: a vehicle without any schedule gives 200 and [] on both sides.
func TestVehicleDataScheduleDataEmpty(t *testing.T) {
	useVehicleDataCache(t, 30)
	useQueue(t, func(ctx context.Context, command string, vin string, body map[string]interface{}, response *models.ApiResponse, autoWakeup bool) error {
		endpoints, _ := body["endpoints"].([]string)
		j, _, err := commands.VehicleDataJSON(ctx, endpoints, func(context.Context, vehicle.StateCategory) (*carserver.VehicleData, error) {
			return &carserver.VehicleData{}, nil
		})
		if err != nil {
			response.Error = err.Error()
		} else {
			response.Result, response.Response = true, j
		}
		response.Finish()
		return nil
	})

	rec := getVehicleData(t, "?endpoints="+url.QueryEscape("charge_schedule_data;preconditioning_schedule_data"))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"charge_schedules":[]`) || !strings.Contains(body, `"precondition_schedules":[]`) || strings.Contains(body, "null") {
		t.Errorf("got %d %s", rec.Code, body)
	}
}

// useDebugLogOutput captures the console output of the logger at Debug level for one test.
func useDebugLogOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	previousLevel := charmlog.GetLevel()
	charmlog.SetOutput(&out)
	charmlog.SetLevel(charmlog.DebugLevel)
	t.Cleanup(func() {
		charmlog.SetOutput(os.Stderr)
		charmlog.SetLevel(previousLevel)
	})
	return &out
}

// storedLogsSince returns the JSON of the stored log entries written after mark, as /api/logs
// serves them (never fmt.Sprint of the fields: a []byte would print as numbers).
func storedLogsSince(t *testing.T, mark int) string {
	t.Helper()
	b, err := json.Marshal(logging.GetStorage().GetRecentEntries(logging.MaxLogEntries)[mark:])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func storedLogsMark(t *testing.T) int {
	t.Helper()
	n := len(logging.GetStorage().GetRecentEntries(logging.MaxLogEntries))
	if n >= logging.MaxLogEntries-100 {
		t.Fatalf("log store nearly full (%d entries): positions are not reliable", n)
	}
	return n
}

// UC1018 AC4: the position read by location_data is never logged: neither the console output nor
// the stored entries served by /api/logs, whether the BLE fetch, the cache or the cache fallback
// answers.
func TestVehicleDataLocationNeverLogged(t *testing.T) {
	forbidden := []string{"48.8583", "2.29448", "Champ de Mars", "latitude", "longitude"}
	useVehicleDataCache(t, 30)
	out := useDebugLogOutput(t)
	queued := vehicleDataQueue(t)
	mark := storedLogsMark(t)

	// Cache miss: BLE fetch.
	rec := getVehicleData(t, "?endpoints=location_data")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "48.85837") {
		t.Fatalf("fetch: got %d %s", rec.Code, rec.Body.String())
	}
	// Valid cache.
	rec = getVehicleData(t, "?endpoints=location_data")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "48.85837") || len(*queued) != 1 {
		t.Fatalf("cache: got %d %s, %d reads", rec.Code, rec.Body.String(), len(*queued))
	}
	// Cache fallback: the BLE fetch of the missing endpoint fails, the cached position is served.
	useQueue(t, func(_ context.Context, _ string, _ string, _ map[string]interface{}, response *models.ApiResponse, _ bool) error {
		response.Error = "vehicle is sleeping"
		response.Finish()
		return nil
	})
	rec = getVehicleData(t, "?endpoints="+url.QueryEscape("location_data;charge_state"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "48.85837") ||
		!strings.Contains(rec.Body.String(), "partially processed from cache") {
		t.Fatalf("fallback: got %d %s", rec.Code, rec.Body.String())
	}

	// GetLogs encodes the stored entries as they are: the JSON of the entries written after mark is
	// what /api/logs serves for them. GetLogs itself is not called here: an earlier test of the
	// package stores a request body holding +Inf, which json cannot encode, so the whole store
	// cannot be served in this process.
	stored := storedLogsSince(t, mark)
	// The scan bites: the logs of these requests are there.
	if !strings.Contains(stored, "location_data") || !strings.Contains(out.String(), "location_data") {
		t.Fatalf("the requests left no log to scan: stored %q, output %q", stored, out.String())
	}
	for name, text := range map[string]string{"storage": stored, "output": out.String()} {
		for _, word := range forbidden {
			if strings.Contains(text, word) {
				t.Errorf("%s contains %q", name, word)
			}
		}
	}
}
