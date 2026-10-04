package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
)

func TestParseBTAdapter(t *testing.T) {
	valid := []struct{ raw, want string }{
		{"", ""},
		{"  ", ""},
		{"hci0", "hci0"},
		{"hci1", "hci1"},
		{"hci12", "hci12"},
		{"hci15", "hci15"},
		{" hci1 ", "hci1"},
	}
	for _, tt := range valid {
		got, err := ParseBTAdapter(tt.raw)
		if err != nil || got != tt.want {
			t.Errorf("ParseBTAdapter(%q) = %q, %v; want %q, nil", tt.raw, got, err, tt.want)
		}
	}
	for _, raw := range []string{"foo", "hci", "hci-1", "hci+1", "hci01", "hci16", "HCI0", "hci 1", "hci1x", "0", "hcifoo"} {
		got, err := ParseBTAdapter(raw)
		if err == nil || got != "" {
			t.Errorf("ParseBTAdapter(%q) = %q, %v; want an error", raw, got, err)
			continue
		}
		for _, part := range []string{fmt.Sprintf("%q", raw), "hci0 to hci15", "empty"} {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("ParseBTAdapter(%q) error %q lacks %q", raw, err, part)
			}
		}
	}
}

func TestParseConnectionTimeout(t *testing.T) {
	tests := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{"", DefaultConnectionTimeout, false},
		{"  ", DefaultConnectionTimeout, false},
		{"10", 10, false},
		{"29", 29, false},
		{"120", 120, false},
		{"+30", 30, false},
		{" 45 ", 45, false},
		{"abc", DefaultConnectionTimeout, true},
		{"9", DefaultConnectionTimeout, true},
		{"121", DefaultConnectionTimeout, true},
		{"0", DefaultConnectionTimeout, true},
		{"-5", DefaultConnectionTimeout, true},
		{"10.5", DefaultConnectionTimeout, true},
		{"1e1", DefaultConnectionTimeout, true},
	}
	for _, tt := range tests {
		got, err := ParseConnectionTimeout(tt.raw)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("ParseConnectionTimeout(%q) = %d, %v; want %d, error %t", tt.raw, got, err, tt.want, tt.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "from 10 to 120") {
			t.Errorf("ParseConnectionTimeout(%q) error %q does not give the range", tt.raw, err)
		}
	}
}

func TestParseReleaseAdapterWhenIdle(t *testing.T) {
	tests := []struct {
		raw     string
		want    bool
		wantErr bool
	}{
		{"", false, false},
		{" ", false, false},
		{"true", true, false},
		{"TRUE", true, false},
		{"1", true, false},
		{"t", true, false},
		{" true ", true, false},
		{"false", false, false},
		{"0", false, false},
		{"yes", false, true},
		{"on", false, true},
		{"abc", false, true},
	}
	for _, tt := range tests {
		got, err := ParseReleaseAdapterWhenIdle(tt.raw)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("ParseReleaseAdapterWhenIdle(%q) = %t, %v; want %t, error %t", tt.raw, got, err, tt.want, tt.wantErr)
		}
	}
}

// logEntriesSince returns the log entries written after mark.
func logEntriesSince(mark int) []logging.LogEntry {
	return logging.GetStorage().GetRecentEntries(logging.MaxLogEntries)[mark:]
}

func TestLoadConfigAdapterOptionsLogs(t *testing.T) {
	mark := func(t *testing.T) int {
		t.Helper()
		n := len(logging.GetStorage().GetRecentEntries(logging.MaxLogEntries))
		if n >= logging.MaxLogEntries-100 {
			t.Fatalf("log store nearly full (%d entries)", n)
		}
		return n
	}
	envFields := func(entries []logging.LogEntry, key string) []interface{} {
		var values []interface{}
		for _, e := range entries {
			if v, ok := e.Fields[key]; ok && e.Message == "Env:" {
				values = append(values, v)
			}
		}
		return values
	}
	mentions := func(entries []logging.LogEntry, name string) bool {
		for _, e := range entries {
			if strings.Contains(e.Message, name) {
				return true
			}
			if _, ok := e.Fields[name]; ok {
				return true
			}
		}
		return false
	}

	t.Run("not set: nothing logged, defaults", func(t *testing.T) {
		t.Setenv("btAdapter", "")
		t.Setenv("connectionTimeout", "")
		t.Setenv("releaseAdapterWhenIdle", "")
		m := mark(t)
		cfg := LoadConfig()
		entries := logEntriesSince(m)
		for _, name := range []string{"btAdapter", "connectionTimeout", "releaseAdapterWhenIdle"} {
			if mentions(entries, name) {
				t.Errorf("%s is logged although not set: %v", name, entries)
			}
		}
		if cfg.BTAdapter != "" || cfg.ConnectionTimeout != DefaultConnectionTimeout || cfg.ReleaseAdapterWhenIdle {
			t.Errorf("defaults = %q, %d, %t", cfg.BTAdapter, cfg.ConnectionTimeout, cfg.ReleaseAdapterWhenIdle)
		}
	})

	t.Run("set: one Env line each", func(t *testing.T) {
		t.Setenv("btAdapter", " hci1 ")
		t.Setenv("connectionTimeout", "10")
		t.Setenv("releaseAdapterWhenIdle", "true")
		m := mark(t)
		cfg := LoadConfig()
		entries := logEntriesSince(m)
		for key, want := range map[string]interface{}{"btAdapter": `"hci1"`, "connectionTimeout": 10, "releaseAdapterWhenIdle": true} {
			if got := envFields(entries, key); len(got) != 1 || got[0] != want {
				t.Errorf("Env %s = %v, want [%v]", key, got, want)
			}
		}
		if cfg.BTAdapter != "hci1" || cfg.ConnectionTimeout != 10 || !cfg.ReleaseAdapterWhenIdle {
			t.Errorf("config = %q, %d, %t", cfg.BTAdapter, cfg.ConnectionTimeout, cfg.ReleaseAdapterWhenIdle)
		}
	})

	t.Run("invalid: warning and defaults", func(t *testing.T) {
		t.Setenv("btAdapter", "")
		t.Setenv("connectionTimeout", "abc")
		t.Setenv("releaseAdapterWhenIdle", "maybe")
		m := mark(t)
		cfg := LoadConfig()
		entries := logEntriesSince(m)
		warned := map[string]string{}
		for _, e := range entries {
			if e.Level == "warn" {
				warned[e.Message], _ = e.Fields["value"].(string)
			}
		}
		if warned["Invalid connectionTimeout value, using default (29)"] != `"abc"` {
			t.Errorf("no connectionTimeout warning naming abc: %v", warned)
		}
		if warned["Invalid releaseAdapterWhenIdle value, adapter release stays disabled"] != `"maybe"` {
			t.Errorf("no releaseAdapterWhenIdle warning naming maybe: %v", warned)
		}
		if got := envFields(entries, "connectionTimeout"); len(got) != 1 || got[0] != 29 {
			t.Errorf("Env connectionTimeout = %v, want [29]", got)
		}
		if cfg.ConnectionTimeout != 29 || cfg.ReleaseAdapterWhenIdle {
			t.Errorf("config = %d, %t", cfg.ConnectionTimeout, cfg.ReleaseAdapterWhenIdle)
		}
	})
}

func TestQuoteEnvValue(t *testing.T) {
	long := strings.Repeat("x", 200)
	for _, tc := range []struct{ in, want string }{
		{"hci1", `"hci1"`},
		{"a\nb", `"a\nb"`},
		{"", `""`},
	} {
		if got := quoteEnvValue(tc.in); got != tc.want {
			t.Errorf("quoteEnvValue(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
	if got := quoteEnvValue(long); len([]rune(got)) != maxLoggedEnvValue {
		t.Errorf("long value logged on %d characters, want %d", len([]rune(got)), maxLoggedEnvValue)
	}
}
