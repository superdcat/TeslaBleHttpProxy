package control

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/wimaha/TeslaBleHttpProxy/config"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// fakeAdapter replaces the SDK calls that open and close the adapter.
type fakeAdapter struct {
	inits    []string // ids the adapter was opened with, in order
	closes   int
	initErr  error
	closeErr error
	events   []string // "init", "close" and, for tests that need it, "scan", in order
}

// useFakeAdapter replaces the seams and resets the sharing guard; everything is restored at the
// end of the test. cfg is the configuration in place for the test.
func useFakeAdapter(t *testing.T, cfg *config.Config) *fakeAdapter {
	t.Helper()
	fake := &fakeAdapter{}
	previousInit, previousClose, previousUnit, previousConfig := initAdapterWithID, closeAdapter, connectionHoldUnit, config.AppConfig
	adapterMu.Lock()
	previousUsers, previousOpen := adapterUsers, adapterOpen
	adapterUsers, adapterOpen = 0, false
	adapterMu.Unlock()
	t.Cleanup(func() {
		initAdapterWithID, closeAdapter, connectionHoldUnit, config.AppConfig = previousInit, previousClose, previousUnit, previousConfig
		adapterMu.Lock()
		adapterUsers, adapterOpen = previousUsers, previousOpen
		adapterMu.Unlock()
	})
	config.AppConfig = cfg
	initAdapterWithID = func(id string) error {
		fake.inits = append(fake.inits, id)
		fake.events = append(fake.events, "init")
		return fake.initErr
	}
	closeAdapter = func() error {
		fake.closes++
		fake.events = append(fake.events, "close")
		return fake.closeErr
	}
	return fake
}

func isAdapterOpen() bool {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	return adapterOpen
}

func TestSetupAdapter(t *testing.T) {
	absent := errors.New("ble: failed to enable device: can't init hci: can't down device: no such device")
	denied := errors.New("can't down device: operation not permitted")
	tests := []struct {
		name      string
		cfg       *config.Config
		initErr   error
		wantInits []string
		wantErr   []string // parts of the error; nil: no error
	}{
		{name: "no configuration", cfg: nil},
		{name: "btAdapter not set", cfg: &config.Config{}},
		{name: "valid", cfg: &config.Config{BTAdapter: "hci1"}, wantInits: []string{"hci1"}},
		{name: "invalid value", cfg: &config.Config{BTAdapter: "foo"}, wantErr: []string{`"foo"`, "hci0 to hci15"}},
		{name: "adapter absent", cfg: &config.Config{BTAdapter: "hci9"}, initErr: absent,
			wantInits: []string{"hci9"}, wantErr: []string{`"hci9"`, "btmgmt info", "no such device"}},
		{name: "not permitted", cfg: &config.Config{BTAdapter: "hci0"}, initErr: denied,
			wantInits: []string{"hci0"}, wantErr: []string{`"hci0"`, "CAP_NET_ADMIN", "setcap", os.Args[0]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useFakeAdapter(t, tt.cfg)
			fake.initErr = tt.initErr
			err := SetupAdapter()
			if !slices.Equal(fake.inits, tt.wantInits) {
				t.Errorf("adapter opened with %v, want %v", fake.inits, tt.wantInits)
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("error %v, want none", err)
				}
				return
			}
			if err == nil {
				t.Fatal("no error")
			}
			for _, part := range tt.wantErr {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("error %q lacks %q", err, part)
				}
			}
			if tt.initErr != nil && !errors.Is(err, tt.initErr) {
				t.Errorf("error %q does not wrap the cause", err)
			}
			if tt.initErr == absent && strings.Contains(err.Error(), "CAP_NET_ADMIN") {
				t.Errorf("error %q advises CAP_NET_ADMIN for an absent adapter", err)
			}
		})
	}
}

func TestAcquireAdapter(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		wantInits []string
	}{
		{name: "no configuration", cfg: nil},
		{name: "no option: no SDK call", cfg: &config.Config{ConnectionTimeout: 10}},
		{name: "btAdapter", cfg: &config.Config{BTAdapter: "hci1"}, wantInits: []string{"hci1"}},
		{name: "release only: default adapter", cfg: &config.Config{ReleaseAdapterWhenIdle: true}, wantInits: []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useFakeAdapter(t, tt.cfg)
			if err := acquireAdapter(); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(fake.inits, tt.wantInits) {
				t.Errorf("adapter opened with %v, want %v", fake.inits, tt.wantInits)
			}
			if isAdapterOpen() != (tt.wantInits != nil) {
				t.Errorf("adapterOpen = %t", isAdapterOpen())
			}
		})
	}

	t.Run("failure", func(t *testing.T) {
		fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1"})
		fake.initErr = errors.New("boom")
		if err := acquireAdapter(); !errors.Is(err, fake.initErr) || isAdapterOpen() {
			t.Errorf("acquireAdapter = %v, open %t; want the failure, not open", err, isAdapterOpen())
		}
	})
}

// The adapter is reopened before the scan window of a connection attempt; a failure is classified
// as before (retry, or the CAP_NET_ADMIN advice).
func TestTryConnectAcquiresAdapterFirst(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantRetry bool
		want      string
	}{
		{name: "retried", err: errors.New("boom"), wantRetry: true, want: "failed to connect to vehicle (A): boom"},
		{name: "not permitted", err: errors.New("can't down device: operation not permitted"), wantRetry: false,
			want: "failed to connect to vehicle (A): can't down device: operation not permitted\nTry again after granting this application CAP_NET_ADMIN:\nsudo setcap 'cap_net_admin=eip' \"$(which " + os.Args[0] + ")\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1", ScanTimeout: 5})
			fake.initErr = tt.err
			bc := newTestQueue()
			conn, car, retry, err := bc.TryConnectToVehicle(context.Background(), &commands.Command{Command: "flash_lights", Vin: testVIN})
			if conn != nil || car != nil || retry != tt.wantRetry || err == nil || err.Error() != tt.want {
				t.Errorf("TryConnectToVehicle = %v, %v, %t, %v; want retry %t and %q", conn, car, retry, err, tt.wantRetry, tt.want)
			}
			if !slices.Equal(fake.inits, []string{"hci1"}) {
				t.Errorf("adapter opened with %v, want hci1", fake.inits)
			}
		})
	}
}

func TestAdapterNotReleasedWhileInUse(t *testing.T) {
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1", ReleaseAdapterWhenIdle: true})
	beginAdapterUse()
	beginAdapterUse()
	if err := acquireAdapter(); err != nil {
		t.Fatal(err)
	}
	endAdapterUse(true)
	if fake.closes != 0 || !isAdapterOpen() {
		t.Fatalf("closed %d times with a user left", fake.closes)
	}
	endAdapterUse(true)
	if fake.closes != 1 || isAdapterOpen() {
		t.Errorf("closed %d times, open %t; want closed once", fake.closes, isAdapterOpen())
	}
	// Extra end calls never take the counter below zero.
	endAdapterUse(true)
	beginAdapterUse()
	endAdapterUse(false)
	if fake.closes != 1 {
		t.Errorf("closed %d times, want 1", fake.closes)
	}
}

func TestReleaseOnlyOnce(t *testing.T) {
	mark := logMark(t)
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1", ReleaseAdapterWhenIdle: true})
	for cycle := 0; cycle < 2; cycle++ {
		beginAdapterUse()
		if err := acquireAdapter(); err != nil {
			t.Fatal(err)
		}
		endAdapterUse(true)
		// Idle again, as after a use that never reopened the adapter.
		beginAdapterUse()
		endAdapterUse(true)
	}
	if fake.closes != 2 || len(fake.inits) != 2 {
		t.Errorf("closes %d, opens %v; want one close per open", fake.closes, fake.inits)
	}
	if lines := logsSince(mark, "Releasing BLE adapter while idle (releaseAdapterWhenIdle)"); len(lines) != 2 || lines[0].Level != "debug" {
		t.Errorf("%d release lines, want 2", len(lines))
	}
}

func TestSetupAdapterReleasesWhenEnabled(t *testing.T) {
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1", ReleaseAdapterWhenIdle: true})
	if err := SetupAdapter(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fake.events, []string{"init", "close"}) || isAdapterOpen() {
		t.Errorf("events %v, open %t; want open then release", fake.events, isAdapterOpen())
	}

	// Without the release the adapter stays open.
	fake = useFakeAdapter(t, &config.Config{BTAdapter: "hci1"})
	if err := SetupAdapter(); err != nil {
		t.Fatal(err)
	}
	if fake.closes != 0 || !isAdapterOpen() {
		t.Errorf("closes %d, open %t; want open", fake.closes, isAdapterOpen())
	}
}

func TestConnectionStatusAcquiresAdapterBeforeScan(t *testing.T) {
	scanner := useFakeScanner(t)
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1"})
	scanner.beacon = testBeacon()
	scan := scanVehicleBeacon
	scanVehicleBeacon = func(ctx context.Context, vin string) (*ble.ScanResult, error) {
		fake.events = append(fake.events, "scan")
		return scan(ctx, vin)
	}
	bc := newTestQueue()
	response := pushStatus(t, bc, context.Background(), testVIN)
	bc.serveNextCommand(nil)
	assertAnswered(t, response, seenJSON)
	if !slices.Equal(fake.events, []string{"init", "scan"}) {
		t.Errorf("events %v, want the adapter opened before the scan", fake.events)
	}
}

func TestConnectionStatusAdapterFailure(t *testing.T) {
	scanner := useFakeScanner(t)
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1"})
	fake.initErr = errors.New("boom")
	bc := newTestQueue()
	response := pushStatus(t, bc, context.Background(), testVIN)
	bc.serveNextCommand(nil)
	assertFinished(t, response, "failed to scan for vehicle: boom")
	if len(scanner.vins) != 0 {
		t.Errorf("scans %v, want none", scanner.vins)
	}
}

// failingConnect makes the connection attempts open the adapter, as the real one does, then fail
// without retry: the command ends and the queue is served.
func failingConnect(fake *fakeVehicle) {
	fake.connect = func(context.Context) (bool, error) {
		_ = acquireAdapter()
		return false, errors.New("fake: no vehicle")
	}
}

func TestIdleQueueReleasesAdapterWhenEnabled(t *testing.T) {
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1", ReleaseAdapterWhenIdle: true})
	failingConnect(useFakeVehicle(t))
	bc := newTestQueue()
	_ = bc.PushCommand(context.Background(), "flash_lights", testVIN, nil, models.NewApiResponse(context.Background()), true)

	bc.serveNextCommand(nil)

	if !slices.Equal(fake.events, []string{"init", "close"}) || adapterUsers != 0 {
		t.Errorf("events %v, users %d; want the adapter opened for the command, then released", fake.events, adapterUsers)
	}
}

func TestQueuedCommandKeepsAdapter(t *testing.T) {
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1", ReleaseAdapterWhenIdle: true})
	failingConnect(useFakeVehicle(t))
	bc := newTestQueue()
	for i := 0; i < 2; i++ {
		_ = bc.PushCommand(context.Background(), "flash_lights", testVIN, nil, models.NewApiResponse(context.Background()), true)
	}

	bc.serveNextCommand(nil)
	if fake.closes != 0 || !isAdapterOpen() {
		t.Fatalf("released with a command waiting (closes %d)", fake.closes)
	}
	bc.serveNextCommand(nil)
	if fake.closes != 1 {
		t.Errorf("closes %d, want one release after the last command", fake.closes)
	}
}

func TestShouldReleaseAdapter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		next   *commands.Command
		queued int
		want   bool
	}{
		{"idle", nil, 0, true},
		{"retry pending", &commands.Command{}, 0, false},
		{"command queued", nil, 1, false},
	} {
		if got := shouldReleaseAdapter(tc.next, tc.queued); got != tc.want {
			t.Errorf("%s: shouldReleaseAdapter = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// endAdapterUse(false) keeps the adapter open.
func TestEndAdapterUseWithoutReleaseKeepsAdapter(t *testing.T) {
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1", ReleaseAdapterWhenIdle: true})
	beginAdapterUse()
	if err := acquireAdapter(); err != nil {
		t.Fatal(err)
	}
	endAdapterUse(false)
	if fake.closes != 0 || !isAdapterOpen() {
		t.Errorf("released with a retry pending (closes %d)", fake.closes)
	}
}

func TestReleaseDisabledByDefault(t *testing.T) {
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1"})
	failingConnect(useFakeVehicle(t))
	bc := newTestQueue()
	_ = bc.PushCommand(context.Background(), "flash_lights", testVIN, nil, models.NewApiResponse(context.Background()), true)
	bc.serveNextCommand(nil)
	if fake.closes != 0 {
		t.Errorf("adapter closed %d times without releaseAdapterWhenIdle", fake.closes)
	}

	// No option at all: the SDK is never called.
	fake = useFakeAdapter(t, &config.Config{})
	_ = bc.PushCommand(context.Background(), "flash_lights", testVIN, nil, models.NewApiResponse(context.Background()), true)
	bc.serveNextCommand(nil)
	if len(fake.events) != 0 {
		t.Errorf("SDK calls %v without any option", fake.events)
	}
}

func TestReleaseFailureIsLogged(t *testing.T) {
	mark := logMark(t)
	fake := useFakeAdapter(t, &config.Config{BTAdapter: "hci1", ReleaseAdapterWhenIdle: true})
	fake.closeErr = errors.New("stop failed")
	beginAdapterUse()
	_ = acquireAdapter()
	endAdapterUse(true)
	lines := logsSince(mark, "Failed to release the BLE adapter")
	if len(lines) != 1 || lines[0].Level != "warn" || lines[0].Fields["error"] != "stop failed" {
		t.Errorf("warning lines %v, want one naming the cause", lines)
	}
}

func TestConnectionHold(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want time.Duration
	}{
		{name: "no configuration", cfg: nil, want: 29 * time.Second},
		{name: "not set", cfg: &config.Config{}, want: 29 * time.Second},
		{name: "negative", cfg: &config.Config{ConnectionTimeout: -1}, want: 29 * time.Second},
		{name: "10", cfg: &config.Config{ConnectionTimeout: 10}, want: 10 * time.Second},
		{name: "120", cfg: &config.Config{ConnectionTimeout: 120}, want: 120 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeAdapter(t, tt.cfg)
			if got := connectionHold(); got != tt.want {
				t.Errorf("connectionHold() = %v, want %v", got, tt.want)
			}
			if got := defaultConnectionHold(); got != 29*time.Second {
				t.Errorf("defaultConnectionHold() = %v, want 29s", got)
			}
		})
	}
}

func TestNewConnectionContexts(t *testing.T) {
	const budget = 29 * time.Second
	t.Run("short hold: the first command keeps its budget", func(t *testing.T) {
		connection, first, cancel := newConnectionContexts(10*time.Second, budget)
		connectionDeadline, _ := connection.Deadline()
		firstDeadline, _ := first.Deadline()
		if gap := firstDeadline.Sub(connectionDeadline); gap < 18*time.Second || gap > 20*time.Second {
			t.Errorf("first command deadline is %v after the hold, want about 19s", gap)
		}
		cancel()
		if connection.Err() == nil || first.Err() == nil {
			t.Error("cancel does not end both contexts")
		}
	})
	for _, hold := range []time.Duration{budget, 120 * time.Second} {
		connection, first, cancel := newConnectionContexts(hold, budget)
		if connection != first {
			t.Errorf("hold %v: the first command has its own context", hold)
		}
		deadline, _ := connection.Deadline()
		if until := time.Until(deadline); until < hold-2*time.Second || until > hold {
			t.Errorf("hold %v: deadline in %v", hold, until)
		}
		cancel()
	}
}

// The only timed test: a connection is closed after its hold, with wide bounds (hold 500 ms).
func TestOperateConnectionHold(t *testing.T) {
	useFakeAdapter(t, &config.Config{ConnectionTimeout: 10})
	connectionHoldUnit = 50 * time.Millisecond
	fake := useFakeVehicle(t)
	bc := newTestQueue()

	begin := time.Now()
	retry := bc.operateConnection(nil, flashLightsCommand(testVIN))
	elapsed := time.Since(begin)

	if retry != nil {
		t.Errorf("operateConnection returned %+v, want nil", retry)
	}
	if !slices.Equal(fake.sends, []string{"flash_lights"}) {
		t.Errorf("sends %v, want the first command", fake.sends)
	}
	if elapsed < 400*time.Millisecond || elapsed > 5*time.Second {
		t.Errorf("connection closed after %v, want about 500ms", elapsed)
	}
}

// A hold expired during a long command closes the connection; the queued command is not taken.
func TestExpiredHoldLeavesQueuedCommand(t *testing.T) {
	useFakeAdapter(t, &config.Config{ConnectionTimeout: 10})
	connectionHoldUnit = time.Millisecond
	fake := useFakeVehicle(t)
	fake.send = func(context.Context) (bool, error) {
		time.Sleep(50 * time.Millisecond) // the hold (10 ms) is over when the command ends
		return false, nil
	}
	bc := newTestQueue()
	_ = bc.PushCommand(context.Background(), "honk_horn", testVIN, nil, nil, true)

	if retry := bc.operateConnection(nil, flashLightsCommand(testVIN)); retry != nil {
		t.Errorf("operateConnection returned %+v, want nil", retry)
	}
	if !slices.Equal(fake.sends, []string{"flash_lights"}) || len(bc.commandStack) != 1 {
		t.Errorf("sends %v, %d queued; want honk_horn left for a new connection", fake.sends, len(bc.commandStack))
	}
}
