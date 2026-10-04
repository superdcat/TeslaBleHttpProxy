package routes

import (
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/handlers"
)

func SetupRoutes(static fs.FS, html fs.FS) *mux.Router {
	router := mux.NewRouter()
	// Optional API token (apiToken, UC1007): runs for the matched routes only.
	router.Use(requireAPIToken)

	// Define the endpoints
	///api/1/vehicles/{vehicle_tag}/command/set_charging_amps
	router.HandleFunc("/api/1/vehicles/{vin}/command/{command}", handlers.Command).Methods("POST")
	router.HandleFunc("/api/1/vehicles/{vin}/vehicle_data", handlers.VehicleData).Methods("GET")
	router.HandleFunc("/api/1/vehicles/{vin}/body_controller_state", handlers.BodyControllerState).Methods("GET")
	router.HandleFunc("/api/proxy/1/version", handlers.Version).Methods("GET")
	router.HandleFunc("/api/proxy/1/capabilities", handlers.Capabilities(func() []string { return proxyRouteNames(router) })).Methods("GET")
	router.HandleFunc("/api/proxy/1/vehicles/{vin}/connection_status", handlers.ConnectionStatus).Methods("GET")
	router.HandleFunc("/dashboard", handlers.ShowDashboard(html)).Methods("GET")
	router.HandleFunc("/logs", handlers.ShowLogViewer(html)).Methods("GET")
	router.HandleFunc("/api/logs", handlers.GetLogs).Methods("GET")
	router.HandleFunc("/api/logs/stats", handlers.GetLogStats).Methods("GET")
	router.HandleFunc("/gen_keys", handlers.GenKeys).Methods("GET")
	router.HandleFunc("/remove_keys", handlers.RemoveKeys).Methods("GET")
	router.HandleFunc("/activate_key", handlers.ActivateKey).Methods("POST")
	router.HandleFunc("/send_key", handlers.SendKey).Methods("POST")
	router.PathPrefix("/static/").Handler(http.FileServer(http.FS(static)))

	// Redirect / to /dashboard (ported from Lenart12/TeslaBleHttpProxy 39f5307)
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

	return router
}

// proxyRoutePrefix is the namespace of the proxy's own routes; keep it in line with
// handlers.proxyAPIVersion (the "api" field of the capabilities).
const proxyRoutePrefix = "/api/proxy/1/"

// proxyRouteNames returns the sorted last path segments of the routes registered under
// proxyRoutePrefix (e.g. "version"). Each such route must end with a unique literal segment.
func proxyRouteNames(router *mux.Router) []string {
	names := []string{}
	_ = router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		template, err := route.GetPathTemplate()
		if err != nil || !strings.HasPrefix(template, proxyRoutePrefix) || len(template) <= len(proxyRoutePrefix) {
			return nil
		}
		names = append(names, path.Base(template))
		return nil
	})
	slices.Sort(names)
	return slices.Compact(names)
}
