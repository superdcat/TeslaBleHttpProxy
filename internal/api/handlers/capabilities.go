package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// proxyAPIVersion is the version of the /api/proxy/1/ namespace; keep it in line with
// routes.proxyRoutePrefix.
const proxyAPIVersion = 1

// Capabilities serves GET /api/proxy/1/capabilities. proxyRoutes lists the registered
// /api/proxy/1/ routes. It is a diagnostic route: it answers without BLE and without key.
func Capabilities(proxyRoutes func() []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logRequest(r, "Capabilities")

		var response models.Response
		response.Command = "capabilities"
		defer commonDefer(w, &response)

		payload, err := json.Marshal(models.Capabilities{
			API:                  proxyAPIVersion,
			Version:              config.Version,
			Flavor:               config.Flavor,
			Commands:             commands.FleetCommandNames(),
			VehicleDataEndpoints: commands.VehicleDataEndpointNames(),
			ProxyRoutes:          proxyRoutes(),
			Features:             capabilityFeatures(),
			KeyRole:              activeKeyRole(),
		})
		if err != nil {
			response.Result = false
			response.Reason = fmt.Sprintf("Failed to marshal capabilities: %s", err)
			return
		}

		response.Result = true
		response.Reason = "The request was successfully processed."
		response.Response = payload
	}
}

func capabilityFeatures() models.CapabilityFeatures {
	return models.CapabilityFeatures{
		StrictBodyValidation:      true,  // UC1003
		BodyControllerStateQueued: true,  // UC1006
		AuthRequired:              false, // UC1007: derived from the token config (guard AppConfig == nil in tests)
	}
}

// activeKeyRole returns the role of the active key, or "" when no key is installed for it.
func activeKeyRole() string {
	role := control.GetActiveKeyRole()
	if !control.KeyExists(role) {
		return ""
	}
	return role
}
