package config

import (
	"runtime/debug"
	"strings"
)

// VehicleCommandModule is the module path of Tesla's vehicle-command SDK.
const VehicleCommandModule = "github.com/teslamotors/vehicle-command"

// SDKVersion returns the version of the vehicle-command SDK linked into the running binary,
// as recorded by the Go toolchain (for example "v0.4.2-0.20260925172039-a4b43c1eff0e").
// An active replace directive is shown as "<version> => <path> <version>".
// It returns "unknown" when the build information is not available.
func SDKVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return sdkVersionFromBuildInfo(info)
}

func sdkVersionFromBuildInfo(info *debug.BuildInfo) string {
	if info == nil {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep == nil || dep.Path != VehicleCommandModule {
			continue
		}
		if r := dep.Replace; r != nil {
			return strings.TrimSpace(dep.Version + " => " + r.Path + " " + r.Version)
		}
		return dep.Version
	}
	return "unknown"
}
