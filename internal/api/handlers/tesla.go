package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/ble/control"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// vehicleDataCacheEntry stores a cached endpoint data with timestamp
type vehicleDataCacheEntry struct {
	data      json.RawMessage // The JSON data for this specific endpoint
	timestamp time.Time
}

// vehicleDataCache is a thread-safe cache for VehicleData endpoints
// Key format: "VIN:endpoint" (e.g., "5YJ3E1EA1JF123456:charge_state")
var (
	vehicleDataCache    = make(map[string]*vehicleDataCacheEntry)
	vehicleDataCacheMux sync.RWMutex
)

func commonDefer(w http.ResponseWriter, response *models.Response) {
	var ret models.Ret
	ret.Response = *response

	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK
	if !response.Result {
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(ret); err != nil {
		// The client may have hung up: never stop the proxy for it.
		logging.Warn("failed to send response", "error", err)
	}
	logging.Debug("Response", "Command", response.Command, "Status", status, "Result", response.Result, "Reason", response.Reason)
}

// enqueueCommand puts a command on the BLE queue, unless ctx ends first (replaced in tests).
var enqueueCommand = func(ctx context.Context, command string, vin string, body map[string]interface{}, response *models.ApiResponse, autoWakeup bool) error {
	return control.BleControlInstance.PushCommand(ctx, command, vin, body, response, autoWakeup)
}

// maxDrainedBody bounds the rest of a request body read before waiting for the BLE queue.
const maxDrainedBody = 64 << 10

// drainBody reads the rest of the request body: net/http watches the connection, and cancels
// the request context when the client hangs up, only once the body has been read to its end.
func drainBody(r *http.Request) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxDrainedBody))
}

// waitForCommand waits until the BLE queue finished the command or the client stops waiting.
// It returns ctx.Err() in the second case; apiResponse must then not be read.
func waitForCommand(ctx context.Context, apiResponse *models.ApiResponse) error {
	select {
	case <-apiResponse.Done():
		return nil
	case <-ctx.Done():
		select {
		case <-apiResponse.Done(): // finished at the same time: keep the outcome
			return nil
		default:
			return ctx.Err()
		}
	}
}

func checkBleControl(response *models.Response) bool {
	if control.BleControlInstance == nil {
		response.Reason = "BleControl is not initialized. Maybe private.pem is missing."
		response.Result = false
		return false
	}
	return true
}

func Command(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	vin := params["vin"]
	command := params["command"]

	wait := r.URL.Query().Get("wait") == "true"
	// Commands always wake up the car automatically (except wake_up itself)
	// The wakeup parameter is ignored for commands, only used for vehicle_data
	autoWakeup := command != "wake_up"

	var response models.Response
	response.Vin = vin
	response.Command = command

	defer commonDefer(w, &response)

	if !checkBleControl(&response) {
		return
	}

	//Body
	var body map[string]interface{} = nil
	decodeErr := json.NewDecoder(r.Body).Decode(&body)
	unreadable := decodeErr != nil && !errors.Is(decodeErr, io.EOF)
	if unreadable && !strings.Contains(decodeErr.Error(), "cannot unmarshal bool") {
		logging.Error("Decoding body", "Error", decodeErr)
	}

	logRequestWithBody(r, "Command", body)

	// A partial decoding (e.g. {"on":true,"x":1e999} gives an UnmarshalTypeError but keeps the
	// keys already read) must never be queued; commands without body ignore the body as in 2.3.0.
	if unreadable {
		body = nil
	}

	if !commands.IsSupportedCommand(command) {
		logging.Error("Command not supported", "Command", command)
		response.Reason = fmt.Sprintf("The command \"%s\" is not supported.", command)
		response.Result = false
		return
	}

	// Refuse an invalid body before queuing it, also with wait=false. The JSON decoding error
	// is logged above; its Go text is not returned to the client.
	if err := commands.ValidateCommandBody(command, body); err != nil {
		if unreadable {
			err = fmt.Errorf("%w: not a valid JSON object", commands.ErrInvalidBody)
		}
		logging.Error("Invalid request body", "Command", command, "Error", err)
		response.Reason = err.Error()
		response.Result = false
		return
	}

	// Complete the validated body once (e.g. the id of add_charge_schedule), so that the retries
	// of the queued command all replay the same body.
	body = commands.PrepareCommandBody(command, body)

	if wait {
		ctx := r.Context()
		drainBody(r)
		apiResponse := models.NewApiResponse(ctx)
		err := enqueueCommand(ctx, command, vin, body, apiResponse, autoWakeup)
		if err == nil {
			err = waitForCommand(ctx, apiResponse)
		}
		if err != nil {
			// The client stopped waiting: it will not read this answer (see UC1005).
			logging.Debug("Client stopped waiting for command", "Command", command, "Reason", err)
			response.Result = false
			response.Reason = err.Error()
			return
		}

		if apiResponse.Result {
			response.Result = true
			response.Reason = "The command was successfully processed."
			response.Response = apiResponse.Response
		} else {
			response.Result = false
			response.Reason = apiResponse.Error
		}
		return
	}

	// wait=false: detached from the request, as in 2.3.0 (blocks while the queue is full).
	if err := enqueueCommand(context.Background(), command, vin, body, nil, autoWakeup); err != nil {
		response.Result = false
		response.Reason = err.Error()
		return
	}
	response.Result = true
	response.Reason = "The command was successfully received and will be processed shortly."
}

// generateVehicleDataCacheKey creates a unique cache key for a specific VIN and endpoint
func generateVehicleDataCacheKey(vin string, endpoint string) string {
	return vin + ":" + endpoint
}

func VehicleData(w http.ResponseWriter, r *http.Request) {
	logRequest(r, "VehicleData")
	params := mux.Vars(r)
	vin := params["vin"]
	command := "vehicle_data"

	var endpoints []string
	endpointsString := r.URL.Query().Get("endpoints")
	if endpointsString != "" {
		endpoints = strings.Split(endpointsString, ";")
	} else {
		endpoints = []string{"charge_state", "climate_state"} //'charge_state', 'climate_state', 'closures_state', 'drive_state', 'gui_settings', 'location_data', 'charge_schedule_data', 'preconditioning_schedule_data', 'vehicle_config', 'vehicle_state', 'vehicle_data_combo'
	}

	var response models.Response
	response.Vin = vin
	response.Command = command

	for _, endpoint := range endpoints {
		if !slices.Contains(commands.ExceptedEndpoints, endpoint) {
			logging.Error("Endpoint not supported", "Endpoint", endpoint)
			response.Reason = fmt.Sprintf("The endpoint \"%s\" is not supported.", endpoint)
			response.Result = false
			commonDefer(w, &response)
			return
		}
	}

	defer commonDefer(w, &response)

	if !checkBleControl(&response) {
		return
	}

	cacheTime := time.Duration(config.AppConfig.VehicleDataCacheTime) * time.Second

	// Check cache for each endpoint
	vehicleDataCacheMux.RLock()
	cachedData := make(map[string]json.RawMessage) // endpoint -> cached data
	missingEndpoints := []string{}

	for _, endpoint := range endpoints {
		cacheKey := generateVehicleDataCacheKey(vin, endpoint)
		cachedEntry, exists := vehicleDataCache[cacheKey]
		if exists {
			age := time.Since(cachedEntry.timestamp)
			if age < cacheTime {
				// Cache hit for this endpoint
				cachedData[endpoint] = cachedEntry.data
				logging.Debug("VehicleData endpoint cache hit", "VIN", vin, "Endpoint", endpoint, "Age", age)
			} else {
				// Cache expired for this endpoint
				logging.Debug("VehicleData endpoint cache expired", "VIN", vin, "Endpoint", endpoint, "Age", age)
				missingEndpoints = append(missingEndpoints, endpoint)
			}
		} else {
			// Cache miss for this endpoint
			logging.Debug("VehicleData endpoint cache miss", "VIN", vin, "Endpoint", endpoint)
			missingEndpoints = append(missingEndpoints, endpoint)
		}
	}
	vehicleDataCacheMux.RUnlock()

	// If all endpoints are cached, construct response from cache
	if len(missingEndpoints) == 0 {
		logging.Debug("VehicleData fully served from cache", "VIN", vin)
		// Build response from cached endpoints
		combinedResponse := make(map[string]json.RawMessage)
		for _, endpoint := range endpoints {
			combinedResponse[endpoint] = cachedData[endpoint]
		}
		responseJson, err := json.Marshal(combinedResponse)
		if err != nil {
			response.Result = false
			response.Reason = fmt.Sprintf("Failed to marshal cached response: %s", err)
			return
		}
		response.Result = true
		response.Reason = "The request was successfully processed."
		response.Response = responseJson
		return
	}

	// Some endpoints missing/expired - fetch from BLE
	ctx := r.Context()
	apiResponse := models.NewApiResponse(ctx)
	autoWakeup := r.URL.Query().Get("wakeup") == "true"
	err := enqueueCommand(ctx, command, vin, map[string]interface{}{"endpoints": endpoints}, apiResponse, autoWakeup)
	if err == nil {
		err = waitForCommand(ctx, apiResponse)
	}
	if err != nil {
		// The client stopped waiting: handled as a failed BLE fetch, as when the queue reports it.
		logging.Debug("Client stopped waiting for vehicle data", "Reason", err)
		apiResponse = &models.ApiResponse{Error: err.Error()}
	}

	if apiResponse.Result {
		// Parse the BLE response to extract individual endpoint data
		var fetchedData map[string]json.RawMessage
		if err := json.Unmarshal(apiResponse.Response, &fetchedData); err != nil {
			response.Result = false
			response.Reason = fmt.Sprintf("Failed to unmarshal BLE response: %s", err)
			return
		}

		// Store each endpoint separately in cache and merge with cached data
		vehicleDataCacheMux.Lock()
		combinedResponse := make(map[string]json.RawMessage)

		// Add cached endpoints that are still valid
		for endpoint, data := range cachedData {
			combinedResponse[endpoint] = data
		}

		// Add freshly fetched endpoints and cache them
		for endpoint, data := range fetchedData {
			combinedResponse[endpoint] = data
			cacheKey := generateVehicleDataCacheKey(vin, endpoint)
			vehicleDataCache[cacheKey] = &vehicleDataCacheEntry{
				data:      data,
				timestamp: time.Now(),
			}
			logging.Debug("VehicleData endpoint cached", "VIN", vin, "Endpoint", endpoint)
		}
		vehicleDataCacheMux.Unlock()

		// Build final response combining cached and fresh data
		responseJson, err := json.Marshal(combinedResponse)
		if err != nil {
			response.Result = false
			response.Reason = fmt.Sprintf("Failed to marshal combined response: %s", err)
			return
		}

		response.Result = true
		response.Reason = "The request was successfully processed."
		response.Response = responseJson
	} else {
		// BLE fetch failed - try to serve from cache if available
		if len(cachedData) > 0 {
			logging.Debug("BLE fetch failed, serving partial data from cache", "VIN", vin, "CachedEndpoints", len(cachedData))
			combinedResponse := make(map[string]json.RawMessage)
			for endpoint, data := range cachedData {
				combinedResponse[endpoint] = data
			}
			responseJson, err := json.Marshal(combinedResponse)
			if err != nil {
				response.Result = false
				response.Reason = apiResponse.Error
				return
			}
			response.Result = true
			response.Reason = "The request was partially processed from cache. Some data may be stale."
			response.Response = responseJson
		} else {
			response.Result = false
			response.Reason = apiResponse.Error
		}
	}
}

// bodyControllerStateTimeout bounds a body_controller_state request, waiting in the BLE queue
// included, as the 15 s of wimaha 2.3.0 (replaced in tests).
var bodyControllerStateTimeout = 15 * time.Second

// BodyControllerState serves GET /api/1/vehicles/{vin}/body_controller_state through the BLE
// queue (VCSEC domain, never wakes the vehicle): it waits for the command in progress and reuses
// its connection for the same VIN.
func BodyControllerState(w http.ResponseWriter, r *http.Request) {
	logRequest(r, "BodyControllerState")
	params := mux.Vars(r)
	vin := params["vin"]

	var response models.Response
	response.Vin = vin
	response.Command = commands.BodyControllerStateCommand

	defer commonDefer(w, &response)

	if !checkBleControl(&response) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), bodyControllerStateTimeout)
	defer cancel()
	drainBody(r)
	apiResponse := models.NewApiResponse(ctx)
	err := enqueueCommand(ctx, commands.BodyControllerStateCommand, vin, nil, apiResponse, false)
	if err == nil {
		err = waitForCommand(ctx, apiResponse)
	}
	if err != nil {
		// Queue busy beyond the deadline, or the client hung up.
		logging.Debug("Stopped waiting for body controller state", "Reason", err)
		response.Result = false
		response.Reason = err.Error()
		return
	}

	if !apiResponse.Result {
		response.Result = false
		response.Reason = apiResponse.Error
		return
	}
	SetCacheControl(w, config.AppConfig.CacheMaxAge)
	response.Result = true
	response.Reason = "The request was successfully processed."
	response.Response = apiResponse.Response
}

func logRequest(r *http.Request, handler string) {
	logging.Debug("Received HTTP request", "Handler", handler, "Method", r.Method, "Endpoint", r.URL, "Client", r.RemoteAddr)
}

func logRequestWithBody(r *http.Request, handler string, body map[string]interface{}) {
	logging.Debug("Received HTTP request", "Handler", handler, "Method", r.Method, "Endpoint", r.URL, "Client", r.RemoteAddr, "Body", body)
}

func SetCacheControl(w http.ResponseWriter, maxAge int) {
	if maxAge > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, must-revalidate", maxAge))
	} else {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
}
