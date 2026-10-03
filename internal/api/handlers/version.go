package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
)

// versionResponse keeps "version" first (a map would sort "flavor" before it).
type versionResponse struct {
	Version string `json:"version"`
	Flavor  string `json:"flavor"`
}

func Version(w http.ResponseWriter, r *http.Request) {
	versionJson, _ := json.Marshal(versionResponse{Version: config.Version, Flavor: config.Flavor})

	response := models.Ret{
		Response: models.Response{
			Result:   true,
			Reason:   "The request was successfully processed.",
			Response: versionJson,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
