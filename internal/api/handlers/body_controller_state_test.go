package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
	"google.golang.org/protobuf/proto"
)

// fixtureAnswerKey is the key of the answer in the fixtures of the JeedomTeslaBLE plugin (French).
const fixtureAnswerKey = "reponse" //nolint:misspell // French key, see testdata/proxy-2.3.0/README.md

// fixture230 is a reference answer of wimaha 2.3.0, copied from the JeedomTeslaBLE plugin
// (tests/fixtures/proxy-2.3.0, see testdata/proxy-2.3.0/README.md).
type fixture230 struct {
	Path   string
	Status int
	Body   json.RawMessage
}

func readFixture230(t *testing.T, name string) fixture230 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "proxy-2.3.0", name))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	var request struct {
		Path string `json:"chemin"`
	}
	var answer struct {
		Status int             `json:"statut_http"`
		Body   json.RawMessage `json:"corps"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal(raw["requete"], &request); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal(raw[fixtureAnswerKey], &answer); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return fixture230{Path: request.Path, Status: answer.Status, Body: answer.Body}
}

// vehicleStatusJSON is what the BLE queue answers for testdata/<name>.binpb of the models golden tests.
func vehicleStatusJSON(t *testing.T, name string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "models", "testdata", name+".binpb"))
	if err != nil {
		t.Fatal(err)
	}
	var vs vcsec.VehicleStatus
	if err := proto.Unmarshal(b, &vs); err != nil {
		t.Fatal(err)
	}
	j, err := commands.BodyControllerStateJSON(&vs)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

type queuedRead struct {
	ctx        context.Context
	command    string
	vin        string
	body       map[string]interface{}
	waited     bool
	autoWakeup bool
}

func getBodyControllerState(t *testing.T, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc("/api/1/vehicles/{vin}/body_controller_state", BodyControllerState).Methods("GET")
	req := httptest.NewRequest(http.MethodGet, "/api/1/vehicles/"+testVIN+"/body_controller_state", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func useCacheMaxAge(t *testing.T, seconds int) {
	t.Helper()
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.Config{CacheMaxAge: seconds}
}

// AC1, AC6: same answers as wimaha 2.3.0, now through the BLE queue (VCSEC read, no wake-up).
func TestBodyControllerStateMatches230Fixtures(t *testing.T) {
	tests := []struct {
		fixture      string
		noKey        bool
		outcome      func(t *testing.T, r *models.ApiResponse)
		cacheControl string
	}{
		{"body_controller_state-eveille.json", false, func(t *testing.T, r *models.ApiResponse) {
			r.Result, r.Response = true, vehicleStatusJSON(t, "vehicle_status_awake")
		}, "public, max-age=5, must-revalidate"},
		{"body_controller_state-endormi.json", false, func(t *testing.T, r *models.ApiResponse) {
			r.Result, r.Response = true, vehicleStatusJSON(t, "vehicle_status_asleep")
		}, "public, max-age=5, must-revalidate"},
		{"body_controller_state-hors-de-portee.json", false, func(_ *testing.T, r *models.ApiResponse) {
			r.Error = "Vehicle is not in range: ble: failed to scan for " + testVIN + ": context deadline exceeded"
		}, ""},
		{"body_controller_state-proxy-sans-cle.json", true, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			want := readFixture230(t, tt.fixture)
			useCacheMaxAge(t, 5)
			var queued []queuedRead
			useQueue(t, func(ctx context.Context, command string, vin string, body map[string]interface{}, response *models.ApiResponse, autoWakeup bool) error {
				queued = append(queued, queuedRead{ctx, command, vin, body, response != nil, autoWakeup})
				tt.outcome(t, response)
				response.Finish()
				return nil
			})
			if tt.noKey {
				control.BleControlInstance = nil
			}

			begin := time.Now()
			rec := getBodyControllerState(t, context.Background())
			end := time.Now()

			if want.Path != "/api/1/vehicles/"+testVIN+"/body_controller_state" {
				t.Fatalf("fixture path %q", want.Path)
			}
			var got, wantBody any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body %q: %v", rec.Body.String(), err)
			}
			if err := json.Unmarshal(want.Body, &wantBody); err != nil {
				t.Fatalf("fixture body %s: %v", want.Body, err)
			}
			if rec.Code != want.Status || !reflect.DeepEqual(got, wantBody) {
				t.Errorf("got %d %s\nwant %d %s", rec.Code, rec.Body.String(), want.Status, want.Body)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != tt.cacheControl {
				t.Errorf("Cache-Control %q, want %q", cc, tt.cacheControl)
			}
			if tt.noKey {
				if len(queued) != 0 {
					t.Errorf("queued %+v without key", queued)
				}
				return
			}
			if len(queued) != 1 {
				t.Fatalf("queued %d commands, want 1", len(queued))
			}
			q := queued[0]
			if q.command != commands.BodyControllerStateCommand || q.vin != testVIN || q.body != nil || !q.waited || q.autoWakeup {
				t.Errorf("queued %+v, want the waited VCSEC read without body nor wake-up", q)
			}
			deadline, ok := q.ctx.Deadline()
			if !ok || deadline.Before(begin.Add(15*time.Second)) || deadline.After(end.Add(15*time.Second)) {
				t.Errorf("queued with deadline %v after the request (set %t), want 15 s", deadline.Sub(begin), ok)
			}
		})
	}
}

// AC3: the queue stays busy (or full) beyond the deadline: 503 context deadline exceeded.
func TestBodyControllerStateTimesOutWhenQueueIsBusy(t *testing.T) {
	if bodyControllerStateTimeout != 15*time.Second {
		t.Fatalf("bodyControllerStateTimeout = %v, want 15 s as in 2.3.0", bodyControllerStateTimeout)
	}
	tests := []struct {
		name    string
		enqueue enqueueFunc
	}{
		{"busy queue", func(_ context.Context, _ string, _ string, _ map[string]interface{}, _ *models.ApiResponse, _ bool) error {
			return nil // accepted, never finished
		}},
		{"full queue", func(ctx context.Context, _ string, _ string, _ map[string]interface{}, _ *models.ApiResponse, _ bool) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				return errors.New("queuing not bounded by the 15 s deadline")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useCacheMaxAge(t, 5)
			previous := bodyControllerStateTimeout
			t.Cleanup(func() { bodyControllerStateTimeout = previous })
			bodyControllerStateTimeout = 100 * time.Millisecond
			useQueue(t, tt.enqueue)

			begin := time.Now()
			rec := getBodyControllerState(t, context.Background())

			if elapsed := time.Since(begin); elapsed > time.Second {
				t.Errorf("answered after %v, want at the deadline", elapsed)
			}
			want := envelope(false, "context deadline exceeded", "body-controller-state")
			if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
				t.Errorf("got %d %s, want 503 %s", rec.Code, rec.Body.String(), want)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "" {
				t.Errorf("Cache-Control %q on a failure", cc)
			}
		})
	}
}
