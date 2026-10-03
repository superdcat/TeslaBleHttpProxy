package config

import (
	"runtime/debug"
	"testing"
)

func TestSDKVersionFromBuildInfo(t *testing.T) {
	sdk := func(version string, replace *debug.Module) *debug.Module {
		return &debug.Module{Path: VehicleCommandModule, Version: version, Replace: replace}
	}
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"no build info", nil, "unknown"},
		{"sdk not linked", &debug.BuildInfo{Deps: []*debug.Module{{Path: "github.com/gorilla/mux", Version: "v1.8.1"}}}, "unknown"},
		{"official pseudo-version", &debug.BuildInfo{Deps: []*debug.Module{nil, sdk("v0.4.2-0.20260925172039-a4b43c1eff0e", nil)}}, "v0.4.2-0.20260925172039-a4b43c1eff0e"},
		{"official tag", &debug.BuildInfo{Deps: []*debug.Module{sdk("v0.4.1", nil)}}, "v0.4.1"},
		{"replaced by a fork", &debug.BuildInfo{Deps: []*debug.Module{sdk("v0.2.1", &debug.Module{Path: "github.com/wimaha/vehicle-command", Version: "v0.0.7"})}}, "v0.2.1 => github.com/wimaha/vehicle-command v0.0.7"},
		{"replaced by a local directory", &debug.BuildInfo{Deps: []*debug.Module{sdk("v0.2.1", &debug.Module{Path: "../vehicle-command"})}}, "v0.2.1 => ../vehicle-command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sdkVersionFromBuildInfo(tt.info); got != tt.want {
				t.Errorf("sdkVersionFromBuildInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}
