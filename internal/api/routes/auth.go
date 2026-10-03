package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
)

// access is how a route is protected when the API token (apiToken, UC1007) is set.
type access int

const (
	accessBearer access = iota // default: API clients (evcc, plugins), Authorization: Bearer <token>
	accessBasic                // browser pages and their requests, HTTP Basic with the token as password
	accessOpen                 // diagnostics and static assets, never authenticated
)

// routeAccess classifies routes by path template; any other route needs the Bearer token, so a
// route added later is protected until it is classified here.
var routeAccess = map[string]access{
	"/api/proxy/1/version":      accessOpen,
	"/api/proxy/1/capabilities": accessOpen,
	"/static/":                  accessOpen, // stylesheet of the pages
	"/":                         accessOpen, // redirect to /dashboard
	"/dashboard":                accessBasic,
	"/logs":                     accessBasic,
	"/api/logs":                 accessBasic,
	"/api/logs/stats":           accessBasic,
	"/gen_keys":                 accessBasic,
	"/remove_keys":              accessBasic,
	"/activate_key":             accessBasic,
	"/send_key":                 accessBasic,
}

const (
	authRealm          = "TeslaBleHttpProxy"
	unauthorizedReason = "unauthorized"
)

// routeTemplate returns the path template of the route matched for r, "?" when unavailable.
func routeTemplate(r *http.Request) string {
	route := mux.CurrentRoute(r)
	if route == nil {
		return "?"
	}
	template, err := route.GetPathTemplate()
	if err != nil {
		return "?"
	}
	return template
}

// accessOf returns the access class of the route matched for r (Bearer when unknown).
func accessOf(r *http.Request) access {
	route := mux.CurrentRoute(r)
	if route == nil {
		return accessBearer
	}
	template, err := route.GetPathTemplate()
	if err != nil {
		return accessBearer
	}
	if a, ok := routeAccess[template]; ok {
		return a
	}
	return accessBearer
}

// requireAPIToken refuses the requests without the API token when it is set; without token
// every request goes through unchanged, as in 2.3.0. Credentials are never logged.
func requireAPIToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := config.CurrentAPIToken()
		if !token.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		class := accessOf(r)
		if class == accessOpen || authorized(r, class, token) {
			next.ServeHTTP(w, r)
			return
		}
		// The route template is a fixed string; the request path is unauthenticated input.
		logging.Warn("Unauthorized request", "Method", r.Method, "Route", routeTemplate(r), "Client", r.RemoteAddr)
		writeUnauthorized(w, class)
	})
}

// authorized checks the credentials of the scheme expected by class: Bearer only for API routes
// (a browser never sends it on its own, so no cross-site request can reuse it), Basic only for
// browser routes (password = token, any user name).
func authorized(r *http.Request, class access, token config.APIToken) bool {
	if class == accessBasic {
		_, password, ok := r.BasicAuth()
		return ok && token.Matches(password)
	}
	scheme, credentials, found := strings.Cut(r.Header.Get("Authorization"), " ")
	return found && strings.EqualFold(scheme, "Bearer") && token.Matches(strings.TrimSpace(credentials))
}

// writeUnauthorized answers HTTP 401 with the standard envelope and the challenge of class.
func writeUnauthorized(w http.ResponseWriter, class access) {
	challenge := `Bearer realm="` + authRealm + `"`
	if class == accessBasic {
		challenge = `Basic realm="` + authRealm + `", charset="UTF-8"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	ret := models.Ret{Response: models.Response{Result: false, Reason: unauthorizedReason}}
	if err := json.NewEncoder(w).Encode(ret); err != nil {
		logging.Warn("failed to send response", "error", err)
	}
}
