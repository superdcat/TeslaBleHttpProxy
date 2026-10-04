package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
)

type enqueueFunc = func(ctx context.Context, command string, vin string, body map[string]interface{}, response *models.ApiResponse, autoWakeup bool) error

// useQueue replaces the BLE queue for one test.
func useQueue(t *testing.T, enqueue enqueueFunc) {
	t.Helper()
	previousInstance, previousEnqueue := control.BleControlInstance, enqueueCommand
	t.Cleanup(func() { control.BleControlInstance, enqueueCommand = previousInstance, previousEnqueue })
	control.BleControlInstance = &control.BleControl{} // only checked for nil by the handlers
	enqueueCommand = enqueue
}

// busyQueue accepts commands and never finishes them, like a queue busy with other commands.
func busyQueue(t *testing.T) chan *models.ApiResponse {
	t.Helper()
	queued := make(chan *models.ApiResponse, 1)
	useQueue(t, func(_ context.Context, _ string, _ string, _ map[string]interface{}, response *models.ApiResponse, _ bool) error {
		queued <- response
		return nil
	})
	return queued
}

func commandRouter() *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/api/1/vehicles/{vin}/command/{command}", Command).Methods("POST")
	router.HandleFunc("/api/1/vehicles/{vin}/vehicle_data", VehicleData).Methods("GET")
	router.HandleFunc("/api/1/vehicles/{vin}/body_controller_state", BodyControllerState).Methods("GET")
	router.HandleFunc("/api/proxy/1/vehicles/{vin}/connection_status", ConnectionStatus).Methods("GET")
	return router
}

func receive(t *testing.T, queued chan *models.ApiResponse) *models.ApiResponse {
	t.Helper()
	select {
	case response := <-queued:
		return response
	case <-time.After(2 * time.Second):
		t.Fatal("nothing queued")
		return nil
	}
}

// AC3: the handler stops waiting as soon as the client hangs up and leaves no goroutine behind.
func TestWaitingHandlersStopWhenClientHangsUp(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		want   string
	}{
		{"command wait=true", http.MethodPost, "/api/1/vehicles/" + testVIN + "/command/door_unlock?wait=true",
			envelope(false, "context canceled", "door_unlock")},
		{"vehicle_data", http.MethodGet, "/api/1/vehicles/" + testVIN + "/vehicle_data",
			envelope(false, "context canceled", "vehicle_data")},
		{"body_controller_state", http.MethodGet, "/api/1/vehicles/" + testVIN + "/body_controller_state",
			envelope(false, "context canceled", "body-controller-state")},
		{"connection_status", http.MethodGet, "/api/proxy/1/vehicles/" + testVIN + "/connection_status",
			envelope(false, "context canceled", "connection_status")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousConfig := config.AppConfig
			t.Cleanup(func() { config.AppConfig = previousConfig })
			config.AppConfig = &config.Config{VehicleDataCacheTime: 0}
			queued := busyQueue(t)
			logging.GetStorage() // starts its cleanup goroutine before the baseline
			baseline := runtime.NumGoroutine()

			ctx, hangUp := context.WithCancel(context.Background())
			req := httptest.NewRequest(tt.method, tt.target, nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			handlerDone := make(chan struct{})
			go func() {
				defer close(handlerDone)
				commandRouter().ServeHTTP(rec, req)
			}()

			response := receive(t, queued)
			time.Sleep(200 * time.Millisecond) // the handler keeps waiting while the queue is busy
			select {
			case <-handlerDone:
				t.Fatal("the handler returned before the command was processed")
			default:
			}

			hangUp()
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("the handler still waits 1 s after the client hung up")
			}
			if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != tt.want {
				t.Errorf("got %d %s, want 503 %s", rec.Code, rec.Body.String(), tt.want)
			}

			deadline := time.Now().Add(time.Second)
			for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if n := runtime.NumGoroutine(); n > baseline {
				t.Errorf("%d goroutine(s) left after the hang-up", n-baseline)
			}
			response.Finish() // the queue reaches the command later: nobody to release, no panic
		})
	}
}

// AC3, AC5 without vehicle: on a real HTTP connection, the hang-up of the client cancels the
// request context, whatever the body length (json.Decoder may stop before the end of the body).
func TestCommandWaitSeesRealClientHangUp(t *testing.T) {
	prefix := `{"charging_amps":16,"pad":"`
	longBody := prefix + strings.Repeat("x", 512-len(prefix)-2) + `"}` + strings.Repeat(" ", 8)
	tests := []struct {
		name string
		body string
	}{
		{"short body", `{"charging_amps":16}`},
		{"JSON value ending at byte 512", longBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queued := busyQueue(t)
			router := commandRouter()
			handlerDone := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				router.ServeHTTP(w, r)
				handlerDone <- struct{}{}
			}))
			t.Cleanup(server.Close)

			ctx, hangUp := context.WithCancel(context.Background())
			t.Cleanup(hangUp) // an early failure must never leave server.Close waiting for the client
			req, err := http.NewRequestWithContext(ctx, http.MethodPost,
				server.URL+"/api/1/vehicles/"+testVIN+"/command/set_charging_amps?wait=true", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				if resp, err := http.DefaultClient.Do(req); err == nil {
					_ = resp.Body.Close()
				}
			}()

			response := receive(t, queued)
			t.Cleanup(response.Finish) // runs before server.Close, which waits for the handler
			hangUp()
			select {
			case <-response.Ctx.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("the request context is not canceled after the client hung up")
			}
			select {
			case <-handlerDone:
			case <-time.After(2 * time.Second):
				t.Fatal("the handler still waits after the client hung up")
			}
		})
	}
}

// AC4: with wait=false the command is queued detached from the request: leaving does not abandon it.
func TestCommandWithoutWaitIsDetachedFromRequest(t *testing.T) {
	var pushCtx context.Context
	var pushed []*models.ApiResponse
	useQueue(t, func(ctx context.Context, _ string, _ string, _ map[string]interface{}, response *models.ApiResponse, _ bool) error {
		pushCtx = ctx
		pushed = append(pushed, response)
		return nil
	})

	ctx, hangUp := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/1/vehicles/"+testVIN+"/command/door_lock", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	commandRouter().ServeHTTP(rec, req)
	hangUp() // the client leaves once answered

	want := envelope(true, "The command was successfully received and will be processed shortly.", "door_lock")
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("got %d %s, want 200 %s", rec.Code, rec.Body.String(), want)
	}
	if len(pushed) != 1 || pushed[0] != nil {
		t.Fatalf("queued responses = %v, want one command without waiting response", pushed)
	}
	if pushCtx.Err() != nil {
		t.Errorf("queuing context ended with the request: %v", pushCtx.Err())
	}
}

// The queue is full and the client gives up: nothing is queued, the handler does not wait.
func TestCommandWaitNotQueuedWhenClientIsGone(t *testing.T) {
	useQueue(t, func(ctx context.Context, _ string, _ string, _ map[string]interface{}, _ *models.ApiResponse, _ bool) error {
		<-ctx.Done() // queue full until the client gives up
		return ctx.Err()
	})
	ctx, hangUp := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, hangUp)
	req := httptest.NewRequest(http.MethodPost, "/api/1/vehicles/"+testVIN+"/command/door_unlock?wait=true", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	commandRouter().ServeHTTP(rec, req)

	if want := envelope(false, "context canceled", "door_unlock"); rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
		t.Errorf("got %d %s, want 503 %s", rec.Code, rec.Body.String(), want)
	}
}

// failingWriter is the response writer of a client that hung up.
type failingWriter struct{ header http.Header }

func (w *failingWriter) Header() http.Header       { return w.header }
func (w *failingWriter) WriteHeader(int)           {}
func (w *failingWriter) Write([]byte) (int, error) { return 0, errors.New("write: broken pipe") }

// Writing the answer to a client that hung up never stops the proxy (2.3.0 called log.Fatal).
func TestCommonDeferSurvivesHungUpClient(t *testing.T) {
	w := &failingWriter{header: http.Header{}}
	commonDefer(w, &models.Response{Result: false, Reason: "context canceled", Command: "door_unlock"})

	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

// When the command finished and the client is gone at the same time, the outcome wins.
func TestWaitForCommandKeepsOutcomeWhenBothReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 100; i++ {
		response := models.NewApiResponse(ctx)
		response.Finish()
		if err := waitForCommand(ctx, response); err != nil {
			t.Fatalf("iteration %d: waitForCommand = %v, want nil", i, err)
		}
	}
}
