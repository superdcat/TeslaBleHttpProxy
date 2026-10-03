package routes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	charmlog "github.com/charmbracelet/log"
	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
)

const (
	testToken = "uc1007-Zq8vR2xKt4Lp9WmN"
	wrongSame = "uc1007-Zq8vR2xKt4Lp9WmX" // same length
	testVIN   = "TESLABLE000000001"
)

// newServedRouter builds the full router with minimal templates, so that /dashboard and /logs
// can be served.
func newServedRouter() *mux.Router {
	static := fstest.MapFS{"static/css/style.css": {Data: []byte("body{}")}}
	html := fstest.MapFS{
		"html/layout.html":    {Data: []byte("page")},
		"html/dashboard.html": {Data: []byte("")},
		"html/logs.html":      {Data: []byte("")},
	}
	return SetupRoutes(static, html)
}

// useToken loads a configuration with raw as apiToken ("" = disabled) for one test.
func useToken(t *testing.T, raw string) {
	t.Helper()
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.Config{CacheMaxAge: 5, VehicleDataCacheTime: 30, APIToken: config.NewAPIToken(raw)}
}

// isolate runs a test without BLE instance, in an empty directory (no key), and empties the
// dashboard message stack afterwards.
func isolate(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	previous := control.BleControlInstance
	control.BleControlInstance = nil
	t.Cleanup(func() {
		control.BleControlInstance = previous
		models.MainMessageStack.PopAll()
	})
}

type routeCase struct {
	method, target, form string
	class                access
	status               int
	body                 string // exact body, or prefix when it ends with "*"
	location             string
}

func notInitialized(command string) string {
	return `{"response":{"result":false,"reason":"BleControl is not initialized. Maybe private.pem is missing.","vin":"` + testVIN + `","command":"` + command + `"}}` + "\n"
}

// frozenRoutes: one request per registered route, chosen so that no handler touches BLE, keys or
// the vehicle, with the 2.3.0 answer.
func frozenRoutes() []routeCase {
	return []routeCase{
		{"POST", "/api/1/vehicles/" + testVIN + "/command/flash_lights", "", accessBearer, 503, notInitialized("flash_lights"), ""},
		{"GET", "/api/1/vehicles/" + testVIN + "/vehicle_data", "", accessBearer, 503, notInitialized("vehicle_data"), ""},
		{"GET", "/api/1/vehicles/" + testVIN + "/body_controller_state", "", accessBearer, 503, notInitialized("body-controller-state"), ""},
		{"GET", "/api/proxy/1/version", "", accessOpen, 200, `{"response":{"result":true,"reason":"The request was successfully processed.","vin":"","command":"","response":{"version":*`, ""},
		{"GET", "/api/proxy/1/capabilities", "", accessOpen, 200, `{"response":{"result":true,"reason":"The request was successfully processed.","vin":"","command":"capabilities"*`, ""},
		{"GET", "/dashboard", "", accessBasic, 200, "page", ""},
		{"GET", "/logs", "", accessBasic, 200, "page", ""},
		{"GET", "/api/logs?limit=1", "", accessBasic, 200, "[*", ""},
		{"GET", "/api/logs/stats", "", accessBasic, 200, `{*`, ""},
		{"GET", "/gen_keys?role=nope", "", accessBasic, 303, "", "/dashboard"},
		{"GET", "/remove_keys", "", accessBasic, 303, "", "/dashboard"},
		{"POST", "/activate_key", "role=nope", accessBasic, 303, "", "/dashboard"},
		{"POST", "/send_key", "VIN=", accessBasic, 303, "", "/dashboard"},
		{"GET", "/static/css/style.css", "", accessOpen, 200, "body{}", ""},
		{"GET", "/", "", accessOpen, 303, "", "/dashboard"},
	}
}

func serve(router http.Handler, tc routeCase, authorization string) *httptest.ResponseRecorder {
	var body io.Reader
	if tc.form != "" {
		body = strings.NewReader(tc.form)
	}
	req := httptest.NewRequest(tc.method, tc.target, body)
	if tc.form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertAnswer(t *testing.T, tc routeCase, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != tc.status {
		t.Fatalf("status %d, want %d (body %q)", rec.Code, tc.status, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("WWW-Authenticate %q, want none", got)
	}
	if got := rec.Header().Get("Location"); got != tc.location {
		t.Errorf("Location %q, want %q", got, tc.location)
	}
	got := rec.Body.String()
	if prefix, ok := strings.CutSuffix(tc.body, "*"); ok {
		if !strings.HasPrefix(got, prefix) {
			t.Errorf("body %q, want prefix %q", got, prefix)
		}
	} else if tc.location == "" && got != tc.body {
		t.Errorf("body %q, want %q", got, tc.body)
	}
}

func basic(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// credentials returns the valid credentials of a class.
func credentials(class access) string {
	switch class {
	case accessBasic:
		return basic("anyone", testToken)
	case accessBearer:
		return "Bearer " + testToken
	}
	return ""
}

func TestRoutesWithoutTokenAnswerAsBefore(t *testing.T) { // AC1, AC8
	for _, withConfig := range []bool{false, true} {
		for _, tc := range frozenRoutes() {
			t.Run(fmt.Sprintf("config=%v %s %s", withConfig, tc.method, tc.target), func(t *testing.T) {
				isolate(t)
				if withConfig {
					useToken(t, "")
				}
				router := newServedRouter()
				assertAnswer(t, tc, serve(router, tc, ""))
				// Credentials are ignored without token.
				assertAnswer(t, tc, serve(router, tc, "Bearer "+testToken))
			})
		}
	}
}

func TestRoutesWithTokenAndCredentialsAnswerAsBefore(t *testing.T) { // AC2, AC3, AC4
	for _, tc := range frozenRoutes() {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			isolate(t)
			useToken(t, testToken)
			router := newServedRouter()
			assertAnswer(t, tc, serve(router, tc, credentials(tc.class)))
			if tc.class == accessBearer {
				assertAnswer(t, tc, serve(router, tc, "bearer   "+testToken))
			}
		})
	}
}

func TestRoutesWithTokenRefuseMissingOrWrongCredentials(t *testing.T) { // AC2, AC3, AC4, AC5
	unauthorized := `{"response":{"result":false,"reason":"unauthorized","vin":"","command":""}}` + "\n"
	attempts := map[string]string{
		"none":              "",
		"wrong same length": "Bearer " + wrongSame,
		"wrong basic":       basic("anyone", wrongSame),
		"token as user":     basic(testToken, ""),
		"other scheme":      "Token " + testToken,
		"bearer empty":      "Bearer ",
		"basic bad base64":  "Basic !!!",
		"bearer no space":   "Bearer",
		"basic on api":      basic("anyone", testToken),
		"bearer on browser": "Bearer " + testToken,
		"prefix":            "Bearer " + testToken[:10],
		"token plus":        "Bearer " + testToken + "x",
	}
	for _, tc := range frozenRoutes() {
		for name, authorization := range attempts {
			t.Run(tc.method+" "+tc.target+" "+name, func(t *testing.T) {
				isolate(t)
				useToken(t, testToken)
				rec := serve(newServedRouter(), tc, authorization)
				if tc.class == accessOpen {
					assertAnswer(t, tc, rec)
					return
				}
				if authorization == credentials(tc.class) {
					return
				}
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status %d, want 401 (body %q)", rec.Code, rec.Body.String())
				}
				want := `Bearer realm="TeslaBleHttpProxy"`
				if tc.class == accessBasic {
					want = `Basic realm="TeslaBleHttpProxy", charset="UTF-8"`
				}
				if got := rec.Header().Get("WWW-Authenticate"); got != want {
					t.Errorf("WWW-Authenticate %q, want %q", got, want)
				}
				if got := rec.Header().Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type %q", got)
				}
				if got := rec.Body.String(); got != unauthorized {
					t.Errorf("body %q, want %q", got, unauthorized)
				}
			})
		}
	}
}

func TestCapabilitiesAuthRequiredWithoutCredentials(t *testing.T) { // AC3, AC6
	for _, raw := range []string{"", testToken} {
		t.Run(fmt.Sprintf("token set=%v", raw != ""), func(t *testing.T) {
			isolate(t)
			useToken(t, raw)
			rec := httptest.NewRecorder()
			newServedRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/proxy/1/capabilities", nil))
			var ret struct {
				Response struct {
					Response struct {
						Features struct {
							AuthRequired *bool `json:"auth_required"`
						} `json:"features"`
					} `json:"response"`
				} `json:"response"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &ret); err != nil || rec.Code != 200 {
				t.Fatalf("status %d, body %q, err %v", rec.Code, rec.Body.String(), err)
			}
			got := ret.Response.Response.Features.AuthRequired
			if got == nil || *got != (raw != "") {
				t.Errorf("apiToken %q: auth_required %v", raw, got)
			}
		})
	}
}

// TestRouteAccessTableMatchesRouter keeps the table in line with the router.
func TestRouteAccessTableMatchesRouter(t *testing.T) {
	seen := map[string]bool{}
	registrations := map[string]int{}
	_ = newTestRouter().Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		template, err := route.GetPathTemplate()
		if err != nil {
			t.Errorf("route without path template")
			return nil
		}
		seen[template] = true
		registrations[template]++
		if _, ok := routeAccess[template]; !ok && !strings.HasPrefix(template, "/api/1/") && !strings.HasPrefix(template, proxyRoutePrefix) {
			t.Errorf("route %q is neither classified nor an API route", template)
		}
		return nil
	})
	for template := range routeAccess {
		if !seen[template] {
			t.Errorf("routeAccess entry %q matches no route", template)
		}
	}
	// A class other than Bearer is attached to a template: a second route sharing it (another
	// method) would inherit it silently.
	for template, class := range routeAccess {
		if class != accessBearer && registrations[template] != 1 {
			t.Errorf("template %q is registered by %d routes, want exactly 1", template, registrations[template])
		}
	}
	if len(seen) != len(frozenRoutes()) {
		t.Errorf("%d routes registered, %d in frozenRoutes", len(seen), len(frozenRoutes()))
	}
}

func TestTokenNeverLogged(t *testing.T) { // AC7
	isolate(t)
	var out bytes.Buffer
	previousLevel := charmlog.GetLevel()
	charmlog.SetOutput(&out)
	charmlog.SetLevel(charmlog.DebugLevel)
	t.Cleanup(func() {
		charmlog.SetOutput(os.Stderr)
		charmlog.SetLevel(previousLevel)
	})

	t.Setenv("apiToken", testToken)
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = config.LoadConfig()

	router := newServedRouter()
	for _, tc := range frozenRoutes() {
		for _, authorization := range []string{"", credentials(tc.class), "Bearer " + wrongSame, basic(testToken, wrongSame)} {
			serve(router, tc, authorization)
		}
	}
	rec := serve(router, routeCase{method: "GET", target: "/api/logs?limit=10000"}, basic("u", testToken))
	if rec.Code != 200 {
		t.Fatalf("/api/logs status %d", rec.Code)
	}
	stored, _ := json.Marshal(logging.GetStorage().GetRecentEntries(logging.MaxLogEntries))
	if !strings.Contains(string(stored), `"apiToken":"set"`) {
		t.Errorf("startup log of apiToken missing")
	}
	secrets := []string{testToken, wrongSame, url.QueryEscape(testToken),
		base64.StdEncoding.EncodeToString([]byte("anyone:" + testToken)), "Bearer", "Basic "}
	for name, text := range map[string]string{"/api/logs": rec.Body.String(), "storage": string(stored), "output": out.String()} {
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Errorf("%s contains %q", name, secret)
			}
		}
	}
	if !strings.Contains(out.String(), "Unauthorized request") {
		t.Errorf("refusals not logged")
	}
}
