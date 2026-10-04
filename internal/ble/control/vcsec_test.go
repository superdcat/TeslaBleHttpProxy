package control

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

const notInRange = "Vehicle is not in range: ble: failed to scan for " + testVIN + ": context deadline exceeded"

func TestVcsecOnlyConnection(t *testing.T) {
	tests := []struct {
		command  commands.Command
		vcsec    bool
		attempts int
	}{
		{commands.Command{Command: commands.BodyControllerStateCommand, Domain: commands.Domain.VCSEC}, true, 1},
		{commands.Command{Command: commands.ConnectionStatusCommand, Domain: commands.Domain.VCSEC}, true, 1},
		{commands.Command{Command: "wake_up"}, false, 3},
		{commands.Command{Command: "wake_up", Domain: commands.Domain.VCSEC}, false, 3},
		{commands.Command{Command: "door_lock"}, false, 3},
		{commands.Command{Command: "vehicle_data"}, false, 3},
	}
	for _, tt := range tests {
		if got := vcsecOnlyConnection(&tt.command); got != tt.vcsec {
			t.Errorf("vcsecOnlyConnection(%s/%q) = %t, want %t", tt.command.Command, tt.command.Domain, got, tt.vcsec)
		}
		if got := connectionAttempts(&tt.command); got != tt.attempts {
			t.Errorf("connectionAttempts(%s/%q) = %d, want %d", tt.command.Command, tt.command.Domain, got, tt.attempts)
		}
	}
}

func TestPushCommandSetsDomain(t *testing.T) {
	bc := newTestQueue()
	_ = bc.PushCommand(context.Background(), commands.BodyControllerStateCommand, testVIN, nil, models.NewApiResponse(context.Background()), false)
	_ = bc.PushCommand(context.Background(), commands.ConnectionStatusCommand, testVIN, nil, models.NewApiResponse(context.Background()), false)
	_ = bc.PushCommand(context.Background(), "door_lock", testVIN, nil, nil, true)
	if got := (<-bc.commandStack).Domain; got != commands.Domain.VCSEC {
		t.Errorf("body controller state domain = %q, want vcsec", got)
	}
	if got := (<-bc.commandStack).Domain; got != commands.Domain.VCSEC {
		t.Errorf("connection status domain = %q, want vcsec", got)
	}
	if got := (<-bc.commandStack).Domain; got != commands.Domain.None {
		t.Errorf("door_lock domain = %q, want none (2.3.0)", got)
	}
}

// recordConnections records the command of each connection attempt, which fails as out of range.
func recordConnections(t *testing.T) *[]commands.Command {
	t.Helper()
	previous := tryConnectToVehicle
	t.Cleanup(func() { tryConnectToVehicle = previous })
	attempts := &[]commands.Command{}
	tryConnectToVehicle = func(_ *BleControl, _ context.Context, command *commands.Command) (*ble.Connection, *vehicle.Vehicle, bool, error) {
		*attempts = append(*attempts, *command)
		return nil, nil, true, errors.New(notInRange)
	}
	return attempts
}

// Queue idle: one VCSEC connection attempt, without wake-up, and the 2.3.0 reason when out of range.
func TestBodyControllerStateConnectsOnceInVcsecDomain(t *testing.T) {
	attempts := recordConnections(t)
	bc := newTestQueue()
	response := models.NewApiResponse(context.Background())
	_ = bc.PushCommand(context.Background(), commands.BodyControllerStateCommand, testVIN, nil, response, false)

	begin := time.Now()
	if retry := bc.serveNextCommand(nil); retry != nil {
		t.Errorf("handed back for retry: %+v", retry)
	}
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Errorf("answered after %v, want no back-off", elapsed)
	}
	if len(*attempts) != 1 {
		t.Fatalf("%d connection attempts, want 1", len(*attempts))
	}
	if a := (*attempts)[0]; a.Domain != commands.Domain.VCSEC || a.AutoWakeup || !vcsecOnlyConnection(&a) {
		t.Errorf("connection for %+v, want VCSEC only without wake-up", a)
	}
	assertFinished(t, response, notInRange)
}

// Connection open for another VIN: it is closed and the read reconnects in the VCSEC domain.
func TestBodyControllerStateOfOtherVinReconnectsVcsecOnly(t *testing.T) {
	fake := useFakeVehicle(t)
	attempts := recordConnections(t)
	bc := newTestQueue()
	response := models.NewApiResponse(context.Background())
	_ = bc.PushCommand(context.Background(), commands.BodyControllerStateCommand, otherVIN, nil, response, false)

	first := &commands.Command{Command: "flash_lights", Vin: testVIN, Response: models.NewApiResponse(context.Background())}
	retry := bc.operateConnection(nil, first)
	if retry == nil || retry.Vin != otherVIN || retry.Domain != commands.Domain.VCSEC {
		t.Fatalf("operateConnection returned %+v, want the VCSEC read of the other VIN", retry)
	}
	bc.serveNextCommand(retry)
	if !slices.Equal(fake.sends, []string{"flash_lights"}) || len(*attempts) != 1 || (*attempts)[0].Domain != commands.Domain.VCSEC {
		t.Errorf("sends %v, attempts %+v; want flash_lights then one VCSEC attempt", fake.sends, *attempts)
	}
	assertFinished(t, response, notInRange)
}

// A VCSEC only connection serves no other command: the next one may need the infotainment session.
func TestVcsecOnlyConnectionIsClosedAfterTheRead(t *testing.T) {
	fake := useFakeVehicle(t)
	bc := newTestQueue()
	_ = bc.PushCommand(context.Background(), "charge_start", testVIN, nil, nil, true)
	first := &commands.Command{Command: commands.BodyControllerStateCommand, Domain: commands.Domain.VCSEC, Vin: testVIN,
		Response: models.NewApiResponse(context.Background())}

	if retry := bc.operateConnection(nil, first); retry != nil {
		t.Errorf("operateConnection returned %+v, want nil", retry)
	}
	if !slices.Equal(fake.sends, []string{commands.BodyControllerStateCommand}) || len(bc.commandStack) != 1 {
		t.Errorf("sends %v, %d queued; want the read only, charge_start left for a new connection", fake.sends, len(bc.commandStack))
	}
}

// AC2: queued behind a command in progress on the same VIN, the read runs after it on its connection.
func TestBodyControllerStateWaitsForCommandInProgress(t *testing.T) {
	fake := useFakeVehicle(t)
	var mu sync.Mutex
	var order []string
	inFlight, maxInFlight := 0, 0
	started, release := make(chan struct{}), make(chan struct{})
	var readDomain commands.DomainType
	sendCommand = func(command *commands.Command, _ context.Context, _ *vehicle.Vehicle) (bool, error) {
		mu.Lock()
		order = append(order, command.Command)
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		if command.Command == commands.BodyControllerStateCommand {
			readDomain = command.Domain
		}
		mu.Unlock()
		if command.Command == "charge_start" {
			close(started)
			<-release
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		return false, nil
	}
	bc := newTestQueue()
	first := &commands.Command{Command: "charge_start", Vin: testVIN, Response: models.NewApiResponse(context.Background())}
	done := make(chan *commands.Command, 1)
	go func() { done <- bc.operateConnection(nil, first) }()

	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	read := models.NewApiResponse(ctx)
	if err := bc.PushCommand(ctx, commands.BodyControllerStateCommand, testVIN, nil, read, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	select {
	case <-read.Done():
		t.Fatal("the read finished while the command was in progress")
	default:
	}
	mu.Lock()
	if len(order) != 1 {
		t.Errorf("sent %v while charge_start was in progress", order)
	}
	mu.Unlock()

	close(release)
	select {
	case <-read.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the read is not served after the command")
	}
	_ = bc.PushCommand(context.Background(), "flash_lights", otherVIN, nil, nil, true) // ends the connection
	<-done

	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(order, []string{"charge_start", commands.BodyControllerStateCommand}) || maxInFlight != 1 {
		t.Errorf("order %v, %d in flight at most; want charge_start then the read, one at a time", order, maxInFlight)
	}
	if !read.Result || readDomain != commands.Domain.VCSEC || len(fake.connects) != 0 {
		t.Errorf("result %t, domain %q, connects %v; want success on the open connection", read.Result, readDomain, fake.connects)
	}
}

// AC3: a read whose deadline passed in the queue is skipped on leaving it: no BLE access at all.
func TestBodyControllerStateExpiredInQueueIsSkipped(t *testing.T) {
	mark := logMark(t)
	fake := useFakeVehicle(t)
	bc := newTestQueue()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	read := models.NewApiResponse(ctx)
	if err := bc.PushCommand(ctx, commands.BodyControllerStateCommand, testVIN, nil, read, false); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()

	if retry := bc.serveNextCommand(nil); retry != nil {
		t.Errorf("expired read handed back for retry: %+v", retry)
	}
	if len(fake.connects) != 0 || len(fake.sends) != 0 {
		t.Errorf("expired read reached the vehicle: connects %v, sends %v", fake.connects, fake.sends)
	}
	assertFinished(t, read, "context deadline exceeded")
	lines := logsSince(mark, abandonedMessage)
	if len(lines) != 1 || lines[0].Fields["Stage"] != stageQueue || lines[0].Fields["Reason"] != context.DeadlineExceeded.Error() {
		t.Errorf("abandoned log lines = %v, want one at stage %q for the deadline", lines, stageQueue)
	}
}
