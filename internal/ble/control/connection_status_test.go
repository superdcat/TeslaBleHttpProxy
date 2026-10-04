package control

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

const (
	seenJSON = `{"local_name":"S0123456789abcdefC","connectable":true,"address":"aa:bb:cc:dd:ee:ff","rssi":-67,"operated":false}`
	// The same beacon served in the open connection.
	operatedJSON = `{"local_name":"S0123456789abcdefC","connectable":true,"address":"aa:bb:cc:dd:ee:ff","rssi":-67,"operated":true}`
)

func testBeacon() *ble.ScanResult {
	return &ble.ScanResult{Address: "aa:bb:cc:dd:ee:ff", LocalName: "S0123456789abcdefC", RSSI: -67, Connectable: true}
}

func absentJSON(vin string) string {
	return `{"local_name":"` + ble.VehicleLocalName(vin) + `","connectable":false,"address":null,"rssi":null,"operated":false}`
}

// fakeScanner replaces the BLE scan: it records the scans and answers with beacon / err, or with
// run when set.
type fakeScanner struct {
	vins      []string
	deadlines []time.Time
	beacon    *ble.ScanResult
	err       error
	run       func(ctx context.Context) (*ble.ScanResult, error)
}

func useFakeScanner(t *testing.T) *fakeScanner {
	t.Helper()
	fake := &fakeScanner{}
	previous, previousFallback, previousConfig := scanVehicleBeacon, connectionStatusScanFallback, config.AppConfig
	t.Cleanup(func() {
		scanVehicleBeacon, connectionStatusScanFallback, config.AppConfig = previous, previousFallback, previousConfig
	})
	config.AppConfig = &config.Config{}
	connectionStatusScanFallback = 50 * time.Millisecond
	scanVehicleBeacon = func(ctx context.Context, vin string) (*ble.ScanResult, error) {
		fake.vins = append(fake.vins, vin)
		deadline, _ := ctx.Deadline()
		fake.deadlines = append(fake.deadlines, deadline)
		if fake.run != nil {
			return fake.run(ctx)
		}
		return fake.beacon, fake.err
	}
	return fake
}

// notSeen scans until the window ends, like the SDK for a vehicle that is not in range.
func notSeen(ctx context.Context) (*ble.ScanResult, error) {
	<-ctx.Done()
	return nil, errors.New("ble: failed to scan for vehicle: " + ctx.Err().Error())
}

func statusCommand(ctx context.Context, vin string) (*commands.Command, *models.ApiResponse) {
	response := models.NewApiResponse(ctx)
	return &commands.Command{Command: commands.ConnectionStatusCommand, Domain: commands.Domain.VCSEC, Vin: vin, Response: response}, response
}

func pushStatus(t *testing.T, bc *BleControl, ctx context.Context, vin string) *models.ApiResponse {
	t.Helper()
	response := models.NewApiResponse(ctx)
	if err := bc.PushCommand(ctx, commands.ConnectionStatusCommand, vin, nil, response, false); err != nil {
		t.Fatal(err)
	}
	return response
}

// assertAnswered checks that the waiting handler was released with a success and this body.
func assertAnswered(t *testing.T, response *models.ApiResponse, want string) {
	t.Helper()
	select {
	case <-response.Done():
	default:
		t.Fatal("the waiting HTTP handler is not released")
	}
	if !response.Result || response.Error != "" || string(response.Response) != want {
		t.Errorf("response = (Result %t, Error %q, %s), want (true, \"\", %s)", response.Result, response.Error, response.Response, want)
	}
}

func assertNoLogContaining(t *testing.T, mark int, forbidden ...string) {
	t.Helper()
	for _, e := range logging.GetStorage().GetRecentEntries(logging.MaxLogEntries)[mark:] {
		for _, f := range forbidden {
			if strings.Contains(strings.ToLower(e.Message), strings.ToLower(f)) {
				t.Errorf("unexpected log line %q", e.Message)
			}
		}
	}
}

func flashLightsCommand(vin string) *commands.Command {
	return &commands.Command{Command: "flash_lights", Vin: vin, Response: models.NewApiResponse(context.Background())}
}

// AC1, AC4: a scan alone answers; nothing connects, no session, no wake-up, lastAwakeTime untouched.
func TestConnectionStatusScansWithoutConnecting(t *testing.T) {
	mark := logMark(t)
	fake := useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	bc := newTestQueue()
	response := pushStatus(t, bc, context.Background(), testVIN)

	if retry := bc.serveNextCommand(nil); retry != nil {
		t.Errorf("handed back for retry: %+v", retry)
	}
	assertAnswered(t, response, seenJSON)
	if !slices.Equal(scanner.vins, []string{testVIN}) {
		t.Errorf("scans of %v, want one scan of %s", scanner.vins, testVIN)
	}
	if len(fake.connects) != 0 || len(fake.sends) != 0 || len(bc.lastAwakeTime) != 0 || bc.operatedBeacon != nil {
		t.Errorf("connects %v, sends %v, lastAwakeTime %v, operatedBeacon %v; want none", fake.connects, fake.sends, bc.lastAwakeTime, bc.operatedBeacon)
	}
	assertNoLogContaining(t, mark, "Connecting to Vehicle", "wake")
}

// AC2: window elapsed with the request alive: a success, not seen, never an error.
func TestConnectionStatusVehicleNotSeen(t *testing.T) {
	useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.run = notSeen
	bc := newTestQueue()
	response := pushStatus(t, bc, context.Background(), testVIN)

	begin := time.Now()
	bc.serveNextCommand(nil)

	assertAnswered(t, response, absentJSON(testVIN))
	if elapsed := time.Since(begin); elapsed < 40*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("answered after %v, want after the 50 ms window", elapsed)
	}
	if len(scanner.vins) != 1 {
		t.Errorf("%d scans, want a single try", len(scanner.vins))
	}
}

func TestConnectionStatusScanWindow(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		want   time.Duration
	}{
		{"scanTimeout 3", &config.Config{ScanTimeout: 3}, 3 * time.Second},
		{"scanTimeout 0", &config.Config{ScanTimeout: 0}, 7 * time.Second},
		{"scanTimeout negative", &config.Config{ScanTimeout: -1}, 7 * time.Second},
		{"no configuration", nil, 7 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeScanner(t)
			connectionStatusScanFallback = 7 * time.Second
			config.AppConfig = tt.config
			if got := connectionStatusScanWindow(); got != tt.want {
				t.Errorf("window = %v, want %v", got, tt.want)
			}
		})
	}
}

// The scan window comes from scanTimeout, and never outlives the request.
func TestConnectionStatusScanDeadline(t *testing.T) {
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	config.AppConfig = &config.Config{ScanTimeout: 3}

	begin := time.Now()
	command, _ := statusCommand(context.Background(), testVIN)
	serveConnectionStatus(command, nil)
	end := time.Now()
	if d := scanner.deadlines[0]; d.Before(begin.Add(3*time.Second)) || d.After(end.Add(3*time.Second)) {
		t.Errorf("scan deadline in %v, want the 3 s of scanTimeout", d.Sub(begin))
	}

	short, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	command, _ = statusCommand(short, testVIN)
	serveConnectionStatus(command, nil)
	if d := scanner.deadlines[1]; d.After(time.Now().Add(500 * time.Millisecond)) {
		t.Errorf("scan deadline in %v, want the 400 ms of the request", time.Until(d))
	}
}

// Adapter or scan error: a failure naming the cause, never a vehicle out of range.
func TestConnectionStatusAdapterError(t *testing.T) {
	mark := logMark(t)
	scanner := useFakeScanner(t)
	scanner.err = errors.New("ble: failed to enable device: not supported on Windows")
	command, response := statusCommand(context.Background(), testVIN)

	serveConnectionStatus(command, nil)

	assertFinished(t, response, "failed to scan for vehicle: ble: failed to enable device: not supported on Windows")
	if len(response.Response) != 0 {
		t.Errorf("body %s on a failure", response.Response)
	}
	if lines := logsSince(mark, "Connection status scan failed"); len(lines) != 1 || lines[0].Level != "error" {
		t.Errorf("log lines %v, want one error line", lines)
	}
}

// The request ends before or during the scan: 503 reasons, never a false "not seen".
func TestConnectionStatusRequestDeadlineDuringScanIsNotAbsent(t *testing.T) {
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	tests := []struct {
		name     string
		ctx      func() (context.Context, context.CancelFunc)
		fallback time.Duration
		want     string
	}{
		{"deadline during the scan", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 30*time.Millisecond)
		}, 5 * time.Second, "context deadline exceeded"},
		{"hang-up during the scan", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(30*time.Millisecond, cancel)
			return ctx, cancel
		}, 5 * time.Second, "context canceled"},
		{"deadline already passed, null window", func() (context.Context, context.CancelFunc) {
			return expired, func() {}
		}, 0, "context deadline exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := useFakeScanner(t)
			scanner.run = notSeen
			connectionStatusScanFallback = tt.fallback
			ctx, cancel := tt.ctx()
			defer cancel()
			command, response := statusCommand(ctx, testVIN)

			serveConnectionStatus(command, nil)

			assertFinished(t, response, tt.want)
		})
	}
}

// AC3: a status whose deadline passed in the queue is skipped on leaving it: no scan at all.
func TestConnectionStatusExpiredInQueueIsSkipped(t *testing.T) {
	mark := logMark(t)
	fake := useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	bc := newTestQueue()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	response := pushStatus(t, bc, ctx, testVIN)
	<-ctx.Done()

	if retry := bc.serveNextCommand(nil); retry != nil {
		t.Errorf("expired status handed back for retry: %+v", retry)
	}
	if len(scanner.vins) != 0 || len(fake.connects) != 0 {
		t.Errorf("expired status reached BLE: scans %v, connects %v", scanner.vins, fake.connects)
	}
	assertFinished(t, response, "context deadline exceeded")
	lines := logsSince(mark, abandonedMessage)
	if len(lines) != 1 || lines[0].Fields["Stage"] != stageQueue {
		t.Errorf("abandoned log lines = %v, want one at stage %q", lines, stageQueue)
	}
}

// A status nobody waits for (no response) is dropped without scanning.
func TestConnectionStatusWithoutResponseIsDropped(t *testing.T) {
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	serveConnectionStatus(&commands.Command{Command: commands.ConnectionStatusCommand, Vin: testVIN}, nil)
	if len(scanner.vins) != 0 {
		t.Errorf("scans %v, want none", scanner.vins)
	}
}

// Handed back for a new connection (retryCommand of serveNextCommand): scanned, never connected.
func TestConnectionStatusAsRetryCommandScans(t *testing.T) {
	fake := useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	command, response := statusCommand(context.Background(), testVIN)

	if retry := newTestQueue().serveNextCommand(command); retry != nil {
		t.Errorf("handed back again: %+v", retry)
	}
	assertAnswered(t, response, seenJSON)
	if len(fake.connects) != 0 || len(scanner.vins) != 1 {
		t.Errorf("connects %v, scans %v; want one scan only", fake.connects, scanner.vins)
	}
}

// Connection open for the same VIN: answered from the scan that opened it, nothing scanned.
func TestConnectionStatusOfConnectedVehicleIsOperated(t *testing.T) {
	fake := useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = &ble.ScanResult{Address: "11:22:33:44:55:66", LocalName: "other", RSSI: -10, Connectable: true} // must not be used
	bc := newTestQueue()
	bc.operatedBeacon = testBeacon() // as TryConnectToVehicle does
	response := pushStatus(t, bc, context.Background(), testVIN)
	_ = bc.PushCommand(context.Background(), "flash_lights", otherVIN, nil, nil, true) // ends the connection

	retry := bc.operateConnection(nil, flashLightsCommand(testVIN))

	assertAnswered(t, response, operatedJSON)
	if len(scanner.vins) != 0 || len(fake.connects) != 0 {
		t.Errorf("scans %v, connects %v; want none", scanner.vins, fake.connects)
	}
	if retry == nil || retry.Vin != otherVIN {
		t.Errorf("operateConnection returned %+v, want the command of the other VIN", retry)
	}
	if !slices.Equal(fake.sends, []string{"flash_lights"}) {
		t.Errorf("sends %v, want flash_lights only (the status is never sent)", fake.sends)
	}
	if bc.operatedBeacon != nil {
		t.Error("operatedBeacon kept after the connection ended")
	}
}

// Connection open for another VIN: closed ("New VIN"), then scanned alone.
func TestConnectionStatusOfOtherVinClosesConnection(t *testing.T) {
	fake := useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	bc := newTestQueue()
	bc.operatedBeacon = testBeacon()
	response := pushStatus(t, bc, context.Background(), otherVIN)

	retry := bc.operateConnection(nil, flashLightsCommand(testVIN))

	if retry == nil || retry.Command != commands.ConnectionStatusCommand || retry.Vin != otherVIN || len(scanner.vins) != 0 {
		t.Fatalf("operateConnection returned %+v after scans %v, want the status handed back unscanned", retry, scanner.vins)
	}
	if bc.serveNextCommand(retry) != nil {
		t.Error("handed back again")
	}
	assertAnswered(t, response, seenJSON)
	if !slices.Equal(scanner.vins, []string{otherVIN}) || len(fake.connects) != 0 {
		t.Errorf("scans %v, connects %v; want one scan of the other VIN", scanner.vins, fake.connects)
	}
}

// Connection open but no scan result recorded (unexpected): warned, closed, then scanned.
func TestConnectionStatusWithoutOperatedBeaconRescans(t *testing.T) {
	mark := logMark(t)
	useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	bc := newTestQueue()
	response := pushStatus(t, bc, context.Background(), testVIN)

	retry := bc.operateConnection(nil, flashLightsCommand(testVIN))

	if retry == nil || retry.Command != commands.ConnectionStatusCommand || len(scanner.vins) != 0 {
		t.Fatalf("operateConnection returned %+v after scans %v, want the status handed back unscanned", retry, scanner.vins)
	}
	bc.serveNextCommand(retry)
	assertAnswered(t, response, seenJSON)
	if len(scanner.vins) != 1 {
		t.Errorf("scans %v, want one", scanner.vins)
	}
	if lines := logsSince(mark, "No scan result for the open connection, closing it"); len(lines) != 1 || lines[0].Level != "warn" {
		t.Errorf("warning lines %v, want one", lines)
	}
}

// Behind a body_controller_state: its VCSEC connection is closed at once, so the status scans.
func TestConnectionStatusBehindBodyControllerStateScans(t *testing.T) {
	fake := useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	bc := newTestQueue()
	bc.operatedBeacon = testBeacon()
	response := pushStatus(t, bc, context.Background(), testVIN)
	first := &commands.Command{Command: commands.BodyControllerStateCommand, Domain: commands.Domain.VCSEC, Vin: testVIN,
		Response: models.NewApiResponse(context.Background())}

	if retry := bc.operateConnection(nil, first); retry != nil {
		t.Errorf("operateConnection returned %+v, want nil", retry)
	}
	if len(bc.commandStack) != 1 || len(scanner.vins) != 0 {
		t.Fatalf("%d queued, scans %v; want the status left in the queue", len(bc.commandStack), scanner.vins)
	}
	bc.serveNextCommand(nil)
	assertAnswered(t, response, seenJSON)
	if len(scanner.vins) != 1 || len(fake.connects) != 0 {
		t.Errorf("scans %v, connects %v; want one scan, no connection", scanner.vins, fake.connects)
	}
}

// Sequence: a connection serves a status as operated; once it ended, the next status scans again
// (a stale operatedBeacon would answer operated:true forever). The fake connection records its
// scan result like TryConnectToVehicle does; it cannot hand a connection back (no adapter), so
// the test then operates it as connectToVehicleAndOperateConnection would.
func TestConnectionStatusSequenceAfterConnectionEnds(t *testing.T) {
	fake := useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	bc := newTestQueue()
	tryConnectToVehicle = func(bc *BleControl, _ context.Context, command *commands.Command) (*ble.Connection, *vehicle.Vehicle, bool, error) {
		fake.connects = append(fake.connects, command.Command)
		bc.operatedBeacon = testBeacon()
		return nil, nil, false, nil
	}
	first := flashLightsCommand(testVIN)
	if _, _, _, err := tryConnectToVehicle(bc, context.Background(), first); err != nil || bc.operatedBeacon == nil {
		t.Fatal("the fake connection does not record the scan result")
	}
	operated := pushStatus(t, bc, context.Background(), testVIN)
	_ = bc.PushCommand(context.Background(), "flash_lights", otherVIN, nil, nil, true) // ends the connection

	bc.operateConnection(nil, first)
	assertAnswered(t, operated, operatedJSON)
	if bc.operatedBeacon != nil {
		t.Fatal("operatedBeacon kept after the connection ended")
	}

	scanned := pushStatus(t, bc, context.Background(), testVIN)
	bc.serveNextCommand(nil) // the status; the command of the other VIN was handed back above
	assertAnswered(t, scanned, seenJSON)
	if len(scanner.vins) != 1 {
		t.Errorf("scans %v, want the second status scanned", scanner.vins)
	}
}

// AC3: queued behind a command in progress, the status waits for it and is answered afterwards,
// one command at a time, without scanning.
func TestConnectionStatusWaitsForCommandInProgress(t *testing.T) {
	fake := useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon = testBeacon()
	started, release := make(chan struct{}), make(chan struct{})
	fake.send = func(context.Context) (bool, error) {
		close(started)
		<-release
		return false, nil
	}
	bc := newTestQueue()
	bc.operatedBeacon = testBeacon()
	done := make(chan *commands.Command, 1)
	go func() { done <- bc.operateConnection(nil, flashLightsCommand(testVIN)) }()

	<-started
	response := pushStatus(t, bc, context.Background(), testVIN)
	time.Sleep(200 * time.Millisecond)
	select {
	case <-response.Done():
		t.Fatal("the status was answered while the command was in progress")
	default:
	}

	close(release)
	select {
	case <-response.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the status is not served after the command")
	}
	_ = bc.PushCommand(context.Background(), "flash_lights", otherVIN, nil, nil, true) // ends the connection
	<-done
	assertAnswered(t, response, operatedJSON)
	if len(scanner.vins) != 0 {
		t.Errorf("scans %v, want none", scanner.vins)
	}
}

// AC3: statuses and commands are served in queue order, one at a time.
func TestConnectionStatusServedInQueueOrder(t *testing.T) {
	tests := []struct {
		name  string
		order []string
		want  []string
	}{
		{"status first", []string{commands.ConnectionStatusCommand, "flash_lights"}, []string{"scan", "connect:flash_lights"}},
		{"command first", []string{"flash_lights", commands.ConnectionStatusCommand}, []string{"connect:flash_lights", "scan"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var events []string
			fake := useFakeVehicle(t)
			scanner := useFakeScanner(t)
			scanner.run = func(context.Context) (*ble.ScanResult, error) {
				events = append(events, "scan")
				return testBeacon(), nil
			}
			previous := tryConnectToVehicle
			tryConnectToVehicle = func(bc *BleControl, ctx context.Context, command *commands.Command) (*ble.Connection, *vehicle.Vehicle, bool, error) {
				events = append(events, "connect:"+command.Command)
				return previous(bc, ctx, command)
			}
			bc := newTestQueue()
			for _, name := range tt.order {
				if err := bc.PushCommand(context.Background(), name, testVIN, nil, models.NewApiResponse(context.Background()), name != commands.ConnectionStatusCommand); err != nil {
					t.Fatal(err)
				}
			}
			bc.serveNextCommand(nil)
			bc.serveNextCommand(nil)
			if !slices.Equal(events, tt.want) {
				t.Errorf("events %v, want %v", events, tt.want)
			}
			if slices.Contains(fake.connects, commands.ConnectionStatusCommand) {
				t.Error("a connection was opened for the status")
			}
		})
	}
}

// A scan that succeeds without a beacon must not panic the queue loop: it is answered as absent.
func TestConnectionStatusNilBeaconWithoutErrorIsAbsent(t *testing.T) {
	useFakeVehicle(t)
	scanner := useFakeScanner(t)
	scanner.beacon, scanner.err = nil, nil
	bc := newTestQueue()
	response := pushStatus(t, bc, context.Background(), testVIN)

	bc.serveNextCommand(nil)

	assertAnswered(t, response, absentJSON(testVIN))
}
