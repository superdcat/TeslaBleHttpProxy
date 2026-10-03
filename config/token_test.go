package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const testSecret = "uc1007-Zq8vR2xKt4Lp9WmN"

func TestNewAPIToken(t *testing.T) {
	tests := []struct {
		raw       string
		enabled   bool
		candidate string
		matches   bool
	}{
		{"", false, "", false},
		{"   ", false, "", false},
		{testSecret, true, testSecret, true},
		{" " + testSecret + "\t", true, testSecret, true},
		{testSecret, true, testSecret[:len(testSecret)-1] + "X", false}, // same length
		{testSecret, true, testSecret + "X", false},
		{testSecret, true, "", false},
		{testSecret, true, " " + testSecret, false}, // the candidate is never trimmed here
	}
	for _, tt := range tests {
		token := NewAPIToken(tt.raw)
		if token.Enabled() != tt.enabled {
			t.Errorf("NewAPIToken(%q).Enabled() = %v, want %v", tt.raw, token.Enabled(), tt.enabled)
		}
		if got := token.Matches(tt.candidate); got != tt.matches {
			t.Errorf("NewAPIToken(%q).Matches(%q) = %v, want %v", tt.raw, tt.candidate, got, tt.matches)
		}
	}
}

func TestAPITokenNeverPrints(t *testing.T) {
	token := NewAPIToken(testSecret)
	digest := hex.EncodeToString(token.digest[:])
	b, _ := json.Marshal(token)
	cfg := &Config{APIToken: token}
	for _, out := range []string{
		fmt.Sprint(token), fmt.Sprintf("%v %+v %#v %s", token, token, token, token),
		fmt.Sprintf("%v %+v %#v", cfg, *cfg, *cfg), string(b),
	} {
		if strings.Contains(out, testSecret) || strings.Contains(out, digest) || strings.Contains(out, fmt.Sprint(token.digest[:])) {
			t.Errorf("token or digest printed: %s", out)
		}
	}
	if got := token.String(); got != "set" {
		t.Errorf("String() = %q, want set", got)
	}
	if got := (APIToken{}).String(); got != "unset" {
		t.Errorf("String() = %q, want unset", got)
	}
}

func TestLoadConfigReadsAPIToken(t *testing.T) {
	t.Setenv("apiToken", testSecret)
	if !LoadConfig().APIToken.Matches(testSecret) {
		t.Errorf("apiToken not loaded")
	}
	t.Setenv("apiToken", "")
	if LoadConfig().APIToken.Enabled() {
		t.Errorf("empty apiToken enables authentication")
	}
	t.Setenv("apiToken", "   ")
	if LoadConfig().APIToken.Enabled() {
		t.Errorf("blank apiToken enables authentication")
	}
}

func TestCurrentAPIToken(t *testing.T) {
	previous := AppConfig
	t.Cleanup(func() { AppConfig = previous })
	AppConfig = nil
	if CurrentAPIToken().Enabled() {
		t.Errorf("enabled without configuration")
	}
	AppConfig = &Config{APIToken: NewAPIToken(testSecret)}
	if !CurrentAPIToken().Matches(testSecret) {
		t.Errorf("configured token not returned")
	}
}
