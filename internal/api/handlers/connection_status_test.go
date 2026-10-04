package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

const connectionStatusPath = "/api/proxy/1/vehicles/" + testVIN + "/connection_status"

const noStore = "no-cache, no-store, must-revalidate"

func getConnectionStatus(ctx context.Context) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	router.HandleFunc("/api/proxy/1/vehicles/{vin}/connection_status", ConnectionStatus).Methods("GET")
	req := httptest.NewRequest(http.MethodGet, connectionStatusPath, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// AC1, AC2: the envelope, byte for byte, for a vehicle seen, absent, and the failures.
func TestConnectionStatusEnvelope(t *testing.T) {
	const processed = "The request was successfully processed."
	seen := `{"local_name":"S0123456789abcdefC","connectable":true,"address":"aa:bb:cc:dd:ee:ff","rssi":-67,"operated":false}`
	absent := `{"local_name":"S0123456789abcdefC","connectable":false,"address":null,"rssi":null,"operated":false}`
	tests := []struct {
		name    string
		noKey   bool
		outcome func(r *models.ApiResponse)
		status  int
		want    string
	}{
		{"seen", false, func(r *models.ApiResponse) { r.Result, r.Response = true, []byte(seen) }, http.StatusOK,
			`{"response":{"result":true,"reason":"` + processed + `","vin":"` + testVIN + `","command":"connection_status","response":` + seen + `}}` + "\n"},
		{"absent", false, func(r *models.ApiResponse) { r.Result, r.Response = true, []byte(absent) }, http.StatusOK,
			`{"response":{"result":true,"reason":"` + processed + `","vin":"` + testVIN + `","command":"connection_status","response":` + absent + `}}` + "\n"},
		{"adapter error", false, func(r *models.ApiResponse) {
			r.Error = "failed to scan for vehicle: ble: failed to enable device: not supported on Windows"
		}, http.StatusServiceUnavailable,
			envelope(false, "failed to scan for vehicle: ble: failed to enable device: not supported on Windows", "connection_status")},
		{"no key", true, nil, http.StatusServiceUnavailable,
			envelope(false, "BleControl is not initialized. Maybe private.pem is missing.", "connection_status")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useCacheMaxAge(t, 5) // the configured cache never applies to this route
			var queued []queuedRead
			useQueue(t, func(ctx context.Context, command string, vin string, body map[string]interface{}, response *models.ApiResponse, autoWakeup bool) error {
				queued = append(queued, queuedRead{ctx, command, vin, body, response != nil, autoWakeup})
				tt.outcome(response)
				response.Finish()
				return nil
			})
			if tt.noKey {
				control.BleControlInstance = nil
			}

			begin := time.Now()
			rec := getConnectionStatus(context.Background())
			end := time.Now()

			if rec.Code != tt.status || rec.Body.String() != tt.want {
				t.Errorf("got %d %s\nwant %d %s", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != noStore {
				t.Errorf("Cache-Control %q, want %q", cc, noStore)
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
			if q.command != commands.ConnectionStatusCommand || q.vin != testVIN || q.body != nil || !q.waited || q.autoWakeup {
				t.Errorf("queued %+v, want the waited status without body nor wake-up", q)
			}
			deadline, ok := q.ctx.Deadline()
			if !ok || deadline.Before(begin.Add(15*time.Second)) || deadline.After(end.Add(15*time.Second)) {
				t.Errorf("queued with deadline %v after the request (set %t), want 15 s", deadline.Sub(begin), ok)
			}
		})
	}
}

// AC3: the queue stays busy (or full) beyond the deadline: 503 context deadline exceeded.
func TestConnectionStatusTimesOutWhenQueueIsBusy(t *testing.T) {
	if connectionStatusTimeout != 15*time.Second {
		t.Fatalf("connectionStatusTimeout = %v, want 15 s", connectionStatusTimeout)
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
			previous := connectionStatusTimeout
			t.Cleanup(func() { connectionStatusTimeout = previous })
			connectionStatusTimeout = 100 * time.Millisecond
			useQueue(t, tt.enqueue)

			begin := time.Now()
			rec := getConnectionStatus(context.Background())

			if elapsed := time.Since(begin); elapsed > time.Second {
				t.Errorf("answered after %v, want at the deadline", elapsed)
			}
			want := envelope(false, "context deadline exceeded", "connection_status")
			if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != want {
				t.Errorf("got %d %s, want 503 %s", rec.Code, rec.Body.String(), want)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != noStore {
				t.Errorf("Cache-Control %q on a failure, want %q", cc, noStore)
			}
		})
	}
}

// The command route still refuses connection_status: it is a read served by the queue scan.
func TestConnectionStatusNotAcceptedOnCommandRoute(t *testing.T) {
	var queued int
	useQueue(t, func(context.Context, string, string, map[string]interface{}, *models.ApiResponse, bool) error {
		queued++
		return nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/1/vehicles/"+testVIN+"/command/connection_status", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	commandRouter().ServeHTTP(rec, req)

	if queued != 0 || !strings.Contains(rec.Body.String(), "not supported") {
		t.Errorf("queued %d, got %d %s; want a refusal as not supported", queued, rec.Code, rec.Body.String())
	}
}
