package control

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

const (
	testVIN  = "TESLABLE000000001"
	otherVIN = "TESLABLE000000002"
)

// fakeVehicle replaces the BLE connection and the SDK: it records what reaches them.
type fakeVehicle struct {
	connects []string                                          // commands a connection was attempted for
	sends    []string                                          // commands handed to the SDK
	connect  func(ctx context.Context) (retry bool, err error) // nil: fails without retry
	send     func(ctx context.Context) (retry bool, err error) // nil: success
}

func useFakeVehicle(t *testing.T) *fakeVehicle {
	t.Helper()
	fake := &fakeVehicle{}
	previousConnect, previousSend := tryConnectToVehicle, sendCommand
	t.Cleanup(func() { tryConnectToVehicle, sendCommand = previousConnect, previousSend })
	tryConnectToVehicle = func(_ *BleControl, ctx context.Context, command *commands.Command) (*ble.Connection, *vehicle.Vehicle, bool, error) {
		fake.connects = append(fake.connects, command.Command)
		if fake.connect == nil {
			return nil, nil, false, errors.New("fake: no vehicle")
		}
		retry, err := fake.connect(ctx)
		return nil, nil, retry, err
	}
	sendCommand = func(command *commands.Command, ctx context.Context, _ *vehicle.Vehicle) (bool, error) {
		fake.sends = append(fake.sends, command.Command)
		if fake.send == nil {
			return false, nil
		}
		return fake.send(ctx)
	}
	return fake
}

func newTestQueue() *BleControl {
	return &BleControl{commandStack: make(chan commands.Command, 50), providerStack: make(chan commands.Command)}
}

// assertFinished checks that the waiting handler was released with a failure.
func assertFinished(t *testing.T, response *models.ApiResponse, wantError string) {
	t.Helper()
	select {
	case <-response.Done():
	default:
		t.Fatal("the waiting HTTP handler is not released")
	}
	if response.Result || response.Error != wantError {
		t.Errorf("response = (Result %t, Error %q), want (false, %q)", response.Result, response.Error, wantError)
	}
}

// logMark returns the position of the next /logs entry. Positions shift once the store holds
// MaxLogEntries entries: the test then stops instead of reading the wrong lines.
func logMark(t *testing.T) int {
	t.Helper()
	n := len(logging.GetStorage().GetRecentEntries(logging.MaxLogEntries))
	if n >= logging.MaxLogEntries-100 {
		t.Fatalf("log store nearly full (%d entries): positions are not reliable", n)
	}
	return n
}

// logsSince returns the /logs entries written after mark with the given message.
func logsSince(mark int, message string) []logging.LogEntry {
	var lines []logging.LogEntry
	for _, e := range logging.GetStorage().GetRecentEntries(logging.MaxLogEntries)[mark:] {
		if e.Message == message {
			lines = append(lines, e)
		}
	}
	return lines
}

const abandonedMessage = "Command abandoned, client stopped waiting"

// assertAbandonedLogged checks the single "Command abandoned" line of /logs written after mark.
func assertAbandonedLogged(t *testing.T, mark int, command string, stage string, attempts int) {
	t.Helper()
	lines := logsSince(mark, abandonedMessage)
	if len(lines) != 1 {
		t.Fatalf("%d abandoned log lines, want 1: %v", len(lines), lines)
	}
	e := lines[0]
	got := fmt.Sprint(e.Fields["Command"], " ", e.Fields["Stage"], " ", e.Fields["Attempts"], " ", e.Fields["Reason"])
	want := fmt.Sprint(command, " ", stage, " ", attempts, " ", context.Canceled)
	if got != want || e.Level != "info" {
		t.Errorf("abandoned log = %s %q, want info %q", e.Level, got, want)
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// AC1: a queued command whose client hung up is never connected nor sent; the next one is served.
func TestAbandonedCommandIsSkippedAtDequeue(t *testing.T) {
	mark := logMark(t)
	fake := useFakeVehicle(t)
	bc := newTestQueue()
	ctx, hangUp := context.WithCancel(context.Background())
	abandoned := models.NewApiResponse(ctx)
	live := models.NewApiResponse(context.Background())
	if err := bc.PushCommand(context.Background(), "door_unlock", testVIN, nil, abandoned, true); err != nil {
		t.Fatal(err)
	}
	if err := bc.PushCommand(context.Background(), "flash_lights", testVIN, nil, live, true); err != nil {
		t.Fatal(err)
	}
	hangUp() // the client gives up while the command waits in the queue

	if retry := bc.serveNextCommand(nil); retry != nil {
		t.Errorf("abandoned command handed back for retry: %q", retry.Command)
	}
	if len(fake.connects) != 0 || len(fake.sends) != 0 {
		t.Fatalf("abandoned command reached the vehicle: connects %v, sends %v", fake.connects, fake.sends)
	}
	assertFinished(t, abandoned, "context canceled")
	assertAbandonedLogged(t, mark, "door_unlock", stageQueue, 0)
	for _, message := range []string{"Connecting to Vehicle ...", "Executing command"} {
		if lines := logsSince(mark, message); len(lines) != 0 {
			t.Errorf("abandoned command logged %q", message)
		}
	}

	bc.serveNextCommand(nil)
	if !slices.Equal(fake.connects, []string{"flash_lights"}) {
		t.Errorf("connects = %v, want [flash_lights]", fake.connects)
	}
	assertFinished(t, live, "fake: no vehicle")
}

// AC1: inside an operated connection, an abandoned command is skipped and the connection serves the next one.
func TestAbandonedCommandIsSkippedInOperatedConnection(t *testing.T) {
	mark := logMark(t)
	fake := useFakeVehicle(t)
	bc := newTestQueue()
	first := &commands.Command{Command: "flash_lights", Vin: testVIN, Response: models.NewApiResponse(context.Background())}
	abandoned := models.NewApiResponse(canceledContext())
	_ = bc.PushCommand(context.Background(), "door_unlock", otherVIN, nil, abandoned, true)
	_ = bc.PushCommand(context.Background(), "honk_horn", testVIN, nil, nil, true)
	_ = bc.PushCommand(context.Background(), "flash_lights", otherVIN, nil, nil, true) // new VIN: ends the connection

	retry := bc.operateConnection(nil, first)

	if retry == nil || retry.Vin != otherVIN || retry.Command != "flash_lights" {
		t.Fatalf("operateConnection returned %+v, want the command of the other VIN", retry)
	}
	if !slices.Equal(fake.sends, []string{"flash_lights", "honk_horn"}) {
		t.Errorf("sends = %v, want [flash_lights honk_horn]", fake.sends)
	}
	assertFinished(t, abandoned, "context canceled")
	assertAbandonedLogged(t, mark, "door_unlock", stageQueue, 0)
}

// AC1: a command whose client hangs up once the vehicle is connected is not sent.
func TestAbandonedCommandIsNotSentAfterConnecting(t *testing.T) {
	mark := logMark(t)
	fake := useFakeVehicle(t)
	command := &commands.Command{Command: "door_unlock", Vin: testVIN, Response: models.NewApiResponse(canceledContext())}

	retry, err, _ := (&BleControl{}).ExecuteCommand(nil, command, context.Background())

	if retry != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("ExecuteCommand = (%v, %v), want (nil, context canceled)", retry, err)
	}
	if len(fake.sends) != 0 {
		t.Errorf("sends = %v, want none", fake.sends)
	}
	assertFinished(t, command.Response, "context canceled")
	assertAbandonedLogged(t, mark, "door_unlock", stageSend, 0)
}

// AC2: retries stop at the next retry point once the client hung up.
func TestRetriesStopWhenClientHangsUp(t *testing.T) {
	tests := []struct {
		name    string
		sendErr string
		hangUp  func(cancel context.CancelFunc) // called during the first sending
	}{
		{"hang-up during the sending", "fake: BLE busy", func(cancel context.CancelFunc) { cancel() }},
		{"hang-up during the back-off", "fake: BLE busy", func(cancel context.CancelFunc) { time.AfterFunc(100*time.Millisecond, cancel) }},
		{"closed pipe after hang-up", "write: closed pipe", func(cancel context.CancelFunc) { cancel() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mark := logMark(t)
			fake := useFakeVehicle(t)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			fake.send = func(context.Context) (bool, error) {
				tt.hangUp(cancel)
				return true, errors.New(tt.sendErr)
			}
			command := &commands.Command{Command: "door_unlock", Vin: testVIN, Response: models.NewApiResponse(ctx)}

			begin := time.Now()
			retry, err, _ := (&BleControl{}).ExecuteCommand(nil, command, context.Background())

			if elapsed := time.Since(begin); elapsed > 2*time.Second {
				t.Errorf("stopped after %v, want before the 3 s back-off ends", elapsed)
			}
			if retry != nil || !errors.Is(err, context.Canceled) {
				t.Errorf("ExecuteCommand = (%v, %v), want (nil, context canceled)", retry, err)
			}
			if len(fake.sends) != 1 {
				t.Errorf("sends = %v, want exactly one", fake.sends)
			}
			assertFinished(t, command.Response, "context canceled")
			assertAbandonedLogged(t, mark, "door_unlock", stageRetry, 1)
		})
	}
}

// AC2: connection retries stop too; nothing is sent.
func TestConnectionRetriesStopWhenClientHangsUp(t *testing.T) {
	tests := []struct {
		name   string
		hangUp func(cancel context.CancelFunc)
	}{
		{"hang-up during the attempt", func(cancel context.CancelFunc) { cancel() }},
		{"hang-up during the back-off", func(cancel context.CancelFunc) { time.AfterFunc(100*time.Millisecond, cancel) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mark := logMark(t)
			fake := useFakeVehicle(t)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			fake.connect = func(context.Context) (bool, error) {
				tt.hangUp(cancel)
				return true, errors.New("fake: not in range")
			}
			command := &commands.Command{Command: "door_unlock", Vin: testVIN, Response: models.NewApiResponse(ctx)}

			begin := time.Now()
			retry := (&BleControl{}).connectToVehicleAndOperateConnection(command)

			if elapsed := time.Since(begin); elapsed > 2*time.Second {
				t.Errorf("stopped after %v, want before the 3 s back-off ends", elapsed)
			}
			if retry != nil || len(fake.connects) != 1 || len(fake.sends) != 0 {
				t.Errorf("retry %v, connects %v, sends %v; want nil, one attempt, nothing sent", retry, fake.connects, fake.sends)
			}
			assertFinished(t, command.Response, "context canceled")
			assertAbandonedLogged(t, mark, "door_unlock", stageConnect, 0)
		})
	}
}

// AC4: a command nobody waits for (wait=false) is never abandoned: it is connected and sent.
func TestDetachedCommandIsAlwaysExecuted(t *testing.T) {
	mark := logMark(t)
	fake := useFakeVehicle(t)
	bc := newTestQueue()
	_ = bc.PushCommand(context.Background(), "door_lock", testVIN, nil, nil, true)

	bc.serveNextCommand(nil)
	if !slices.Equal(fake.connects, []string{"door_lock"}) {
		t.Errorf("connects = %v, want [door_lock]", fake.connects)
	}

	retry, err, _ := bc.ExecuteCommand(nil, &commands.Command{Command: "door_lock", Vin: testVIN}, context.Background())
	if retry != nil || err != nil || !slices.Equal(fake.sends, []string{"door_lock"}) {
		t.Errorf("ExecuteCommand = (%v, %v), sends %v; want sent once", retry, err, fake.sends)
	}
	if lines := logsSince(mark, abandonedMessage); len(lines) != 0 {
		t.Errorf("detached command logged as abandoned: %v", lines)
	}
}

func TestPushCommandGivesUpWhenClientIsGone(t *testing.T) {
	for i := 0; i < 50; i++ {
		free := &BleControl{commandStack: make(chan commands.Command, 1)}
		if err := free.PushCommand(canceledContext(), "door_unlock", testVIN, nil, nil, true); !errors.Is(err, context.Canceled) || len(free.commandStack) != 0 {
			t.Fatalf("canceled before, iteration %d: err %v, queued %d; want context canceled, nothing queued", i, err, len(free.commandStack))
		}
	}

	bc := &BleControl{commandStack: make(chan commands.Command, 1)}

	bc.commandStack <- commands.Command{Command: "flash_lights"} // queue full
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if err := bc.PushCommand(ctx, "door_unlock", testVIN, nil, nil, true); !errors.Is(err, context.Canceled) || len(bc.commandStack) != 1 {
		t.Errorf("queue full: err %v, queued %d; want context canceled, nothing more queued", err, len(bc.commandStack))
	}

	// A command accepted by the queue has not been sent yet.
	free := &BleControl{commandStack: make(chan commands.Command, 1)}
	if err := free.PushCommand(context.Background(), "door_unlock", testVIN, nil, nil, true); err != nil {
		t.Fatalf("PushCommand: %v", err)
	}
	if queued := <-free.commandStack; queued.SendAttempts != 0 {
		t.Errorf("SendAttempts = %d after PushCommand, want 0", queued.SendAttempts)
	}
}

// A command handed back for a new connection (it may have been sent) is skipped before reconnecting.
func TestRequeuedAbandonedCommandIsSkipped(t *testing.T) {
	mark := logMark(t)
	fake := useFakeVehicle(t)
	command := &commands.Command{Command: "door_unlock", Vin: testVIN, Response: models.NewApiResponse(canceledContext()), SendAttempts: 1}

	if retry := newTestQueue().serveNextCommand(command); retry != nil {
		t.Errorf("requeued command handed back again: %q", retry.Command)
	}
	if len(fake.connects) != 0 || len(fake.sends) != 0 {
		t.Errorf("connects %v, sends %v; want none", fake.connects, fake.sends)
	}
	assertFinished(t, command.Response, "context canceled")
	assertAbandonedLogged(t, mark, "door_unlock", stageRequeue, 1)
	if lines := logsSince(mark, "Retrying command"); len(lines) != 0 {
		t.Errorf("abandoned command logged as retried")
	}
}

// Closed pipe on a command of an open connection, client still waiting: the command is not
// retried (as in 2.3.0) but its handler is released with the failure instead of waiting forever.
func TestClosedPipeInOperatedConnectionReleasesHandler(t *testing.T) {
	fake := useFakeVehicle(t)
	fake.send = func(context.Context) (bool, error) {
		if len(fake.sends) == 2 {
			return true, errors.New("write: closed pipe")
		}
		return false, nil
	}
	bc := newTestQueue()
	first := &commands.Command{Command: "flash_lights", Vin: testVIN, Response: models.NewApiResponse(context.Background())}
	waiting := models.NewApiResponse(context.Background())
	_ = bc.PushCommand(context.Background(), "door_lock", testVIN, nil, waiting, true)
	_ = bc.PushCommand(context.Background(), "flash_lights", otherVIN, nil, nil, true) // new VIN: ends the connection

	if retry := bc.operateConnection(nil, first); retry == nil || retry.Vin != otherVIN {
		t.Fatalf("operateConnection returned %+v, want the command of the other VIN", retry)
	}
	if !slices.Equal(fake.sends, []string{"flash_lights", "door_lock"}) {
		t.Errorf("sends = %v, want [flash_lights door_lock] (no retry)", fake.sends)
	}
	assertFinished(t, waiting, "write: closed pipe")
}
