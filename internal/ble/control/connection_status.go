// Connection status of the vehicle (BLE scan only) served by the BLE queue.
//
// Adapted from Lenart12/TeslaBleHttpProxy (connection_status route, commits 6ca3e0e and
// b971a05), Copyright Lenart12 and contributors, Apache License 2.0. Rewritten in the superdcat
// fork for the queue of wimaha 2.3.0 (no copy of Lenart12's control.go).

package control

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// scanVehicleBeacon is the BLE scan, replaced in tests (no adapter).
var scanVehicleBeacon = ble.ScanVehicleBeacon

// connectionStatusScanFallback is the scan window when scanTimeout is not positive (replaced in tests).
var connectionStatusScanFallback = 5 * time.Second

// connectionStatusScanWindow is the duration of the scan of a connection_status: scanTimeout, or
// connectionStatusScanFallback when it is not positive or the configuration is missing.
func connectionStatusScanWindow() time.Duration {
	if config.AppConfig != nil && config.AppConfig.ScanTimeout > 0 {
		return time.Duration(config.AppConfig.ScanTimeout) * time.Second
	}
	return connectionStatusScanFallback
}

// connectionStatusOf builds the response from a scan result; a nil beacon means the vehicle was
// not seen.
func connectionStatusOf(vin string, beacon *ble.ScanResult, operated bool) models.ConnectionStatus {
	status := models.ConnectionStatus{LocalName: ble.VehicleLocalName(vin), Operated: operated}
	if beacon != nil {
		address, rssi := beacon.Address, beacon.RSSI
		status.LocalName = beacon.LocalName
		status.Connectable = beacon.Connectable
		status.Address = &address
		status.RSSI = &rssi
	}
	return status
}

// failConnectionStatus answers a connection_status whose scan could not run.
func failConnectionStatus(response *models.ApiResponse, err error) {
	logging.Error("Connection status scan failed", "Error", err)
	response.Error = "failed to scan for vehicle: " + err.Error()
	response.Result = false
	response.Finish()
}

// serveConnectionStatus answers a connection_status command and releases its waiting handler. With
// operated (the scan that opened the connection the queue holds for this VIN) it scans nothing;
// otherwise it scans once, for the window of connectionStatusScanWindow. It never connects.
func serveConnectionStatus(command *commands.Command, operated *ble.ScanResult) {
	if command.Response == nil {
		return // nobody waits for the answer
	}
	response := command.Response
	beacon, isOperated := operated, operated != nil
	if !isOperated {
		if err := acquireAdapter(); err != nil {
			if skipAbandonedCommand(command, stageConnect) {
				return
			}
			failConnectionStatus(response, err)
			return
		}
		parent := response.Ctx
		if parent == nil {
			parent = context.Background()
		}
		scanCtx, cancel := context.WithTimeout(parent, connectionStatusScanWindow())
		defer cancel()
		logging.Debug("Scanning for vehicle (connection status) ...")
		var err error
		beacon, err = scanVehicleBeacon(scanCtx, command.Vin)
		if err != nil {
			// The request ended (deadline, hang-up): never answered as a vehicle out of range.
			if skipAbandonedCommand(command, stageConnect) {
				return
			}
			if scanCtx.Err() == nil {
				failConnectionStatus(response, err)
				return
			}
			// Scan window elapsed with the request alive: beacon not seen.
			beacon = nil
		} else if beacon != nil {
			logging.Debug("Beacon found", "LocalName", beacon.LocalName, "Address", beacon.Address, "RSSI", beacon.RSSI)
		}
	}

	status := connectionStatusOf(command.Vin, beacon, isOperated)
	body, err := json.Marshal(status)
	if err != nil {
		response.Error = "failed to marshal connection_status: " + err.Error()
		response.Result = false
		response.Finish()
		return
	}
	response.Response = body
	response.Result = true
	args := []interface{}{"Command", command.Command, "Present", beacon != nil, "Connectable", status.Connectable, "Operated", isOperated}
	if status.RSSI != nil {
		args = append(args, "RSSI", *status.RSSI)
	}
	logging.Info("Successfully executed", args...)
	response.Finish()
}
