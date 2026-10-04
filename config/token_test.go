package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
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

// UC1018 D-1018-02: an unset apiToken logs one recommendation (Info); a blank one keeps its
// warning only; a token logs neither. The "Env: apiToken" line stays as before.
func TestLoadConfigRecommendsAPIToken(t *testing.T) {
	tests := []struct {
		name            string
		raw             string
		wantEnv         string
		wantRecommended int
		wantWarnings    int
	}{
		{"unset", "", "unset", 1, 0},
		{"blank", "   ", "unset", 0, 1},
		{"token", testSecret, "set", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("apiToken", tt.raw)
			mark := len(logging.GetStorage().GetRecentEntries(logging.MaxLogEntries))
			LoadConfig()
			entries := logging.GetStorage().GetRecentEntries(logging.MaxLogEntries)[mark:]
			recommended, warnings, env := 0, 0, 0
			for _, e := range entries {
				switch {
				case e.Level == "info" && e.Message == apiTokenRecommendation:
					recommended++
				case e.Level == "warn" && e.Message == "apiToken is blank: authentication stays disabled":
					warnings++
				case e.Message == "Env:" && e.Fields["apiToken"] != nil:
					env++
					if e.Fields["apiToken"] != tt.wantEnv {
						t.Errorf("Env apiToken = %v, want %s", e.Fields["apiToken"], tt.wantEnv)
					}
				}
				if strings.Contains(e.Message, testSecret) {
					t.Errorf("token logged: %q", e.Message)
				}
			}
			if recommended != tt.wantRecommended || warnings != tt.wantWarnings || env != 1 {
				t.Errorf("%d recommendations, %d warnings, %d Env lines; want %d, %d, 1",
					recommended, warnings, env, tt.wantRecommended, tt.wantWarnings)
			}
		})
	}
	if !strings.Contains(apiTokenRecommendation, "location_data") {
		t.Errorf("recommendation does not mention the location")
	}
}
