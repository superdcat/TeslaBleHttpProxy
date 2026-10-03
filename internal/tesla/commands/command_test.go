package commands

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
)

func TestCommandAbandoned(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	alive, cancelAlive := context.WithCancel(context.Background())
	defer cancelAlive()

	tests := []struct {
		name     string
		response *models.ApiResponse
		want     error
	}{
		{"no response (wait=false)", nil, nil},
		{"response without context", &models.ApiResponse{}, nil},
		{"client still waiting", models.NewApiResponse(alive), nil},
		{"client hung up", models.NewApiResponse(canceled), context.Canceled},
		{"client deadline passed", models.NewApiResponse(expired), context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Command{Command: "door_unlock", Response: tt.response}
			if got := c.Abandoned(); !errors.Is(got, tt.want) || (tt.want == nil && got != nil) {
				t.Fatalf("Abandoned() = %v, want %v", got, tt.want)
			}
			if c.SendAttempts != 0 {
				t.Fatalf("Abandoned() must not change SendAttempts, got %d", c.SendAttempts)
			}
		})
	}
}

func TestCommandDomain(t *testing.T) {
	tests := []struct {
		command string
		want    DomainType
	}{
		{BodyControllerStateCommand, Domain.VCSEC},
		{"wake_up", Domain.None},
		{"door_lock", Domain.None},
		{"actuate_trunk", Domain.None},
		{"window_control", Domain.None},
		{"vehicle_data", Domain.None},
		{"session_info", Domain.None},
		{"body_controller_state", Domain.None},
	}
	for _, tt := range tests {
		if got := CommandDomain(tt.command); got != tt.want {
			t.Errorf("CommandDomain(%q) = %q, want %q", tt.command, got, tt.want)
		}
	}
}

func TestBodyControllerStateIsQueuedOnly(t *testing.T) {
	if !sendSwitchCases(t)[BodyControllerStateCommand] {
		t.Errorf("Send has no case %q", BodyControllerStateCommand)
	}
	if IsSupportedCommand(BodyControllerStateCommand) {
		t.Errorf("%q must not be accepted on the command route", BodyControllerStateCommand)
	}
}
