package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// connectionStatusTimeout bounds a connection_status request, waiting in the BLE queue included
// (replaced in tests).
var connectionStatusTimeout = 15 * time.Second

// ConnectionStatus serves GET /api/proxy/1/vehicles/{vin}/connection_status through the BLE queue:
// a scan alone tells whether the vehicle is in range and with which signal. It opens no session
// and never wakes the vehicle; the answer is never cached.
func ConnectionStatus(w http.ResponseWriter, r *http.Request) {
	logRequest(r, "ConnectionStatus")
	vin := mux.Vars(r)["vin"]

	var response models.Response
	response.Vin = vin
	response.Command = commands.ConnectionStatusCommand

	SetCacheControl(w, 0)
	defer commonDefer(w, &response)

	if !checkBleControl(&response) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), connectionStatusTimeout)
	defer cancel()
	drainBody(r)
	apiResponse := models.NewApiResponse(ctx)
	err := enqueueCommand(ctx, commands.ConnectionStatusCommand, vin, nil, apiResponse, false)
	if err == nil {
		err = waitForCommand(ctx, apiResponse)
	}
	if err != nil {
		// Queue busy beyond the deadline, or the client hung up.
		logging.Debug("Stopped waiting for connection status", "Reason", err)
		response.Result = false
		response.Reason = err.Error()
		return
	}

	if !apiResponse.Result {
		response.Result = false
		response.Reason = apiResponse.Error
		return
	}
	response.Result = true
	response.Reason = "The request was successfully processed."
	response.Response = apiResponse.Response
}
