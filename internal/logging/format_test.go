package logging

// These tests freeze the console log format and the shape of the stored log entries.
// They must pass unchanged before and after a charmbracelet/log upgrade (UC1023).
// Never rewrite the expected text blindly: a difference is a behavior change to review.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	charmlog "github.com/charmbracelet/log"
)

// captureConsole redirects the default charm logger to a buffer with a fixed clock,
// no color and the Info level; the previous state is restored at cleanup.
func captureConsole(t *testing.T) *bytes.Buffer {
	t.Helper()
	// No ANSI sequences even if the environment forces colors.
	t.Setenv("CLICOLOR_FORCE", "")
	t.Setenv("FORCE_COLOR", "")

	fixed := time.Date(2026, 10, 4, 12, 34, 56, 0, time.UTC)
	previous := charmlog.GetLevel()
	var out bytes.Buffer
	charmlog.SetOutput(&out)
	charmlog.SetTimeFunction(func(time.Time) time.Time { return fixed })
	charmlog.SetLevel(charmlog.InfoLevel)
	t.Cleanup(func() {
		// TimeFunction is a transformation whose default is the identity, not a clock.
		charmlog.SetTimeFunction(func(t time.Time) time.Time { return t })
		charmlog.SetOutput(os.Stderr)
		charmlog.SetLevel(previous)
	})
	return &out
}

func TestConsoleLogFormat(t *testing.T) {
	out := captureConsole(t)

	Infof("TeslaBleHttpProxy %s is loading ...", "1.2.3")
	Info("Env:", "httpListenAddress", ":8080")
	Debug("hidden at info level", "Key", "value")
	SetLevel(charmlog.DebugLevel)
	Debug("Command", "Command", "set_charging_amps", "Body", map[string]float64{"charging_amps": 16.5})
	Warn("Command failed", "Command", "door_lock", "Error", errors.New("vehicle is sleeping"), "Attempts", 3)
	Error("refused", "Key", "a b c", "Quote", `he said "x"`, "Empty", "")
	Info("odd", "lonely")

	want := strings.Join([]string{
		`2026/10/04 12:34:56 INFO TeslaBleHttpProxy 1.2.3 is loading ...`,
		`2026/10/04 12:34:56 INFO Env: httpListenAddress=:8080`,
		`2026/10/04 12:34:56 DEBU Command Command=set_charging_amps Body=map[charging_amps:16.5]`,
		`2026/10/04 12:34:56 WARN Command failed Command=door_lock Error="vehicle is sleeping" Attempts=3`,
		`2026/10/04 12:34:56 ERRO refused Key="a b c" Quote="he said \"x\"" Empty=""`,
		`2026/10/04 12:34:56 INFO odd lonely="missing value"`,
		``,
	}, "\n")
	if got := out.String(); got != want {
		t.Errorf("console log format changed\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestConsoleLogEscapesForgedInput(t *testing.T) {
	out := captureConsole(t)

	forged := "2026/10/04 12:34:56 INFO forged"
	Warn("x", "Reason", "a\n"+forged+"\x1b[31m")
	Warn("y", "Reason", "\x1b")

	got := out.String()
	want := strings.Join([]string{
		`2026/10/04 12:34:56 WARN x`,
		`  Reason=`,
		`  │ a`,
		`  │ 2026/10/04 12:34:56 INFO forged\x1b[31m`,
		`2026/10/04 12:34:56 WARN y Reason="\x1b"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("escaping of forged input changed\n got: %q\nwant: %q", got, want)
	}
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("output contains a raw ESC byte: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, forged) {
			t.Errorf("a log line starts with the forged line: %q", line)
		}
	}
}

func TestParseLevel(t *testing.T) {
	for name, want := range map[string]charmlog.Level{
		"debug": charmlog.DebugLevel,
		"info":  charmlog.InfoLevel,
		"warn":  charmlog.WarnLevel,
		"error": charmlog.ErrorLevel,
	} {
		got, err := charmlog.ParseLevel(name)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v, nil", name, got, err, want)
		}
	}
	if _, err := charmlog.ParseLevel("verbose"); err == nil {
		t.Error("ParseLevel(verbose) succeeded, want an error")
	}
}

func TestStoredLogEntryFields(t *testing.T) {
	// With io.Discard, charmbracelet/log v1 skips formatting, but CaptureLog runs
	// before it: the stored entry stays valid. Do not "fix" this test.
	previous := charmlog.GetLevel()
	charmlog.SetOutput(io.Discard)
	charmlog.SetLevel(charmlog.InfoLevel)
	t.Cleanup(func() {
		charmlog.SetOutput(os.Stderr)
		charmlog.SetLevel(previous)
	})

	store := GetStorage()
	before := len(store.GetRecentEntries(MaxLogEntries))
	Warn("Command failed", "Command", "door_lock", "Error", errors.New("vehicle is sleeping"))
	entries := store.GetRecentEntries(MaxLogEntries)
	if len(entries) != before+1 {
		t.Fatalf("stored %d entries, want %d", len(entries), before+1)
	}
	raw, err := json.Marshal(entries[len(entries)-1])
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}
	if _, ok := got["timestamp"].(string); !ok {
		t.Errorf("timestamp = %v, want a string", got["timestamp"])
	}
	if got["level"] != "warn" || got["message"] != "Command failed" {
		t.Errorf("level/message = %v/%v, want warn/Command failed", got["level"], got["message"])
	}
	fields, _ := got["fields"].(map[string]any)
	if len(fields) != 2 || fields["Command"] != "door_lock" || fields["Error"] != "vehicle is sleeping" {
		t.Errorf("fields = %v, want Command=door_lock Error=\"vehicle is sleeping\"", got["fields"])
	}
}
