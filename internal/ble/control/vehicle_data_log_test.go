package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// UC1018 AC4: the BLE queue logs a vehicle_data command (Executing, Successfully executed) with
// its body, never the response the vehicle filled in: the position read by location_data stays
// out of the logs.
func TestVehicleDataResponseNeverLogged(t *testing.T) {
	const position = `{"location_data":{"timestamp":1,"latitude":43.29651,"longitude":5.37016,"location_name":"Vieux-Port Marseille"}}`
	forbidden := []string{"43.29651", "5.37016", "Vieux-Port Marseille", "latitude", "longitude"}
	useFakeVehicle(t)
	sendCommand = func(command *commands.Command, _ context.Context, _ *vehicle.Vehicle) (bool, error) {
		command.Response.Result = true
		command.Response.Response = json.RawMessage(position)
		return false, nil
	}
	bc := newTestQueue()
	response := models.NewApiResponse(context.Background())
	command := &commands.Command{
		Command:  "vehicle_data",
		Vin:      testVIN,
		Body:     map[string]interface{}{"endpoints": []string{"location_data"}},
		Response: response,
	}

	mark := logMark(t)
	retry, err, _ := bc.ExecuteCommand(nil, command, context.Background())
	if retry != nil || err != nil {
		t.Fatalf("ExecuteCommand = (%v, %v)", retry, err)
	}
	if string(response.Response) != position {
		t.Fatalf("the fake vehicle did not fill the response: %s", response.Response)
	}

	b, err := json.Marshal(logging.GetStorage().GetRecentEntries(logging.MaxLogEntries)[mark:])
	if err != nil {
		t.Fatal(err)
	}
	stored := string(b)
	if !strings.Contains(stored, "Executing command") || !strings.Contains(stored, "Successfully executed") || !strings.Contains(stored, "location_data") {
		t.Fatalf("the command left no log to scan: %s", stored)
	}
	for _, word := range forbidden {
		if strings.Contains(stored, word) {
			t.Errorf("logs contain %q: %s", word, stored)
		}
	}
}
