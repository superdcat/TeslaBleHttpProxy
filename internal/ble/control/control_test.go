package control

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// A non-retryable error is reported as a failure; 2.3.0 turned it into a success.
// Only commands that fail before reaching the SDK are used: the vehicle is nil.
func TestExecuteCommandReportsNonRetryableError(t *testing.T) {
	tests := []struct {
		name    string
		command string
		body    map[string]interface{}
		want    string
	}{
		{"invalid body", "set_charging_amps", map[string]interface{}{}, "invalid request body: charging_amps missing"},
		{"invalid sentry body", "set_sentry_mode", map[string]interface{}{"on": "yes"}, "invalid request body: on is not a valid boolean"},
		{"vehicle_data without endpoints", "vehicle_data", nil, "request body is nil"},
		{"unknown command", "nope", nil, "unrecognized command: nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wg sync.WaitGroup
			wg.Add(1)
			response := &models.ApiResponse{Wait: &wg, Ctx: context.Background()}
			command := &commands.Command{Command: tt.command, Vin: "TESLABLE000000001", Body: tt.body, Response: response}

			retryCommand, err, _ := (&BleControl{}).ExecuteCommand(nil, command, context.Background())

			if err == nil || err.Error() != tt.want {
				t.Errorf("ExecuteCommand error = %v, want %q", err, tt.want)
			}
			if retryCommand != nil {
				t.Errorf("ExecuteCommand asks to retry %q", retryCommand.Command)
			}
			if response.Result || response.Error != tt.want {
				t.Errorf("response = (Result %t, Error %q), want (false, %q)", response.Result, response.Error, tt.want)
			}
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("the waiting HTTP handler is never released")
			}
		})
	}
}
