package routes

import (
	"embed"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/config"
)

// newTestRouter builds the router with empty embedded file systems.
// Handlers that render templates (/dashboard, /logs) must never be served
// with it: only route matching is checked for them.
func newTestRouter() *mux.Router {
	return SetupRoutes(embed.FS{}, embed.FS{})
}

func TestRootRedirectsToDashboard(t *testing.T) {
	router := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET / returned status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if location := rec.Header().Get("Location"); location != "/dashboard" {
		t.Errorf("GET / returned Location %q, want %q", location, "/dashboard")
	}
}

func TestVersionRouteServesInjectedVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "fork prerelease", version: "2.3.0-tb.0"},
		{name: "upstream release", version: "2.3.0"},
		{name: "default", version: "*undefined*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := config.Version
			config.Version = tt.version
			t.Cleanup(func() { config.Version = previous })

			router := newTestRouter()
			req := httptest.NewRequest(http.MethodGet, "/api/proxy/1/version", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status %d, want %d", rec.Code, http.StatusOK)
			}
			if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
				t.Errorf("Content-Type %q, want %q", contentType, "application/json")
			}

			want := `{"response":{"result":true,"reason":"The request was successfully processed.","vin":"","command":"","response":{"version":"` +
				tt.version + `"}}}` + "\n"
			if got := rec.Body.String(); got != want {
				t.Errorf("body mismatch\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestFrozenRoutesRegistered(t *testing.T) {
	router := newTestRouter()

	// For a non-matching request, gorilla/mux returns false and sets MatchErr:
	// ErrMethodMismatch when the path exists with another method, ErrNotFound
	// otherwise.
	tests := []struct {
		method    string
		path      string
		wantMatch bool
		wantErr   error
	}{
		{http.MethodPost, "/api/1/vehicles/VIN/command/flash_lights", true, nil},
		{http.MethodGet, "/api/1/vehicles/VIN/vehicle_data", true, nil},
		{http.MethodGet, "/api/1/vehicles/VIN/body_controller_state", true, nil},
		{http.MethodGet, "/api/proxy/1/version", true, nil},
		{http.MethodGet, "/dashboard", true, nil},
		{http.MethodGet, "/logs", true, nil},
		{http.MethodGet, "/api/logs", true, nil},
		{http.MethodGet, "/api/logs/stats", true, nil},
		{http.MethodGet, "/gen_keys", true, nil},
		{http.MethodGet, "/remove_keys", true, nil},
		{http.MethodPost, "/activate_key", true, nil},
		{http.MethodPost, "/send_key", true, nil},
		{http.MethodGet, "/static/x", true, nil},
		{http.MethodGet, "/nope", false, mux.ErrNotFound},
		{http.MethodGet, "/send_key", false, mux.ErrMethodMismatch},
		{http.MethodPost, "/dashboard", false, mux.ErrMethodMismatch},
		{http.MethodPost, "/api/proxy/1/version", false, mux.ErrMethodMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			var match mux.RouteMatch
			matched := router.Match(req, &match)

			if !tt.wantMatch {
				if matched {
					t.Errorf("%s %s matched a route, want no match", tt.method, tt.path)
				}
				if !errors.Is(match.MatchErr, tt.wantErr) {
					t.Errorf("%s %s match error %v, want %v", tt.method, tt.path, match.MatchErr, tt.wantErr)
				}
				return
			}
			if !matched {
				t.Fatalf("%s %s matched no route", tt.method, tt.path)
			}
			if match.MatchErr != nil {
				t.Errorf("%s %s match error: %v", tt.method, tt.path, match.MatchErr)
			}
		})
	}
}
