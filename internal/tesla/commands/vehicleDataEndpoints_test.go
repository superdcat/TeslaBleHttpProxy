package commands

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/carserver"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
	"github.com/wimaha/TeslaBleHttpProxy/internal/api/models"
)

func TestVehicleDataEndpointRegistry(t *testing.T) {
	names := VehicleDataEndpointNames()
	if !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Errorf("VehicleDataEndpointNames() = %v, want sorted without duplicate", names)
	}
	for _, name := range names {
		endpoint := vehicleDataEndpoints[name]
		if endpoint.convert == nil {
			t.Errorf("endpoint %q has no converter", name)
			continue
		}
		j, err := json.Marshal(endpoint.convert(&carserver.VehicleData{}))
		if err != nil || len(j) == 0 || j[0] != '{' {
			t.Errorf("endpoint %q converts an empty VehicleData to %s (%v), want a JSON object", name, j, err)
		}
		if !IsSupportedEndpoint(name) {
			t.Errorf("IsSupportedEndpoint(%q) = false", name)
		}
	}

	// The result is a copy: changing it must not alter the next call.
	want := slices.Clone(names)
	names[0] = "changed"
	if again := VehicleDataEndpointNames(); !slices.Equal(again, want) {
		t.Errorf("second call = %v, want %v", again, want)
	}

	for _, name := range []string{"drive", "Drive_State", "DRIVE_STATE", "closures", "Closures_State", "closure_state", "closures-state", "closures_state;", "", "charge-schedule", "tire-pressure", "nope"} {
		if IsSupportedEndpoint(name) {
			t.Errorf("IsSupportedEndpoint(%q) = true, want false", name)
		}
	}
}

func TestVehicleDataEndpointCategories(t *testing.T) {
	want := map[string]vehicle.StateCategory{
		"charge_state":   vehicle.StateCategoryCharge,
		"climate_state":  vehicle.StateCategoryClimate,
		"drive_state":    vehicle.StateCategoryDrive,
		"closures_state": vehicle.StateCategoryClosures,
	}
	if len(vehicleDataEndpoints) != len(want) {
		t.Errorf("registry has %d endpoints, want %d", len(vehicleDataEndpoints), len(want))
	}
	for name, category := range want {
		if got := vehicleDataEndpoints[name].category; got != category {
			t.Errorf("category of %q = %v, want %v", name, got, category)
		}
	}
}

func TestDefaultVehicleDataEndpoints(t *testing.T) {
	got := DefaultVehicleDataEndpoints()
	if want := []string{"charge_state", "climate_state"}; !slices.Equal(got, want) {
		t.Fatalf("DefaultVehicleDataEndpoints() = %v, want %v", got, want)
	}
	for _, name := range got {
		if !IsSupportedEndpoint(name) {
			t.Errorf("default endpoint %q is not registered", name)
		}
	}
	got[0] = "changed"
	if again := DefaultVehicleDataEndpoints(); again[0] != "charge_state" {
		t.Errorf("second call = %v, the default was modified through the returned slice", again)
	}
}

// The API only grows: removing one of these endpoints must be a deliberate change of this test.
func TestVehicleDataEndpointNamesFloor(t *testing.T) {
	names := VehicleDataEndpointNames()
	for _, endpoint := range []string{"charge_state", "climate_state", "drive_state", "closures_state"} {
		if !slices.Contains(names, endpoint) {
			t.Errorf("endpoint %q is missing from VehicleDataEndpointNames()", endpoint)
		}
	}
}

func TestVehicleDataJSON(t *testing.T) {
	vd := &carserver.VehicleData{}
	var categories []vehicle.StateCategory
	read := func(_ context.Context, c vehicle.StateCategory) (*carserver.VehicleData, error) {
		categories = append(categories, c)
		return vd, nil
	}

	got, retry, err := VehicleDataJSON(context.Background(), []string{"charge_state", "drive_state"}, read)
	if err != nil || retry {
		t.Fatalf("VehicleDataJSON() = (%v, %v)", retry, err)
	}
	if want := []vehicle.StateCategory{vehicle.StateCategoryCharge, vehicle.StateCategoryDrive}; !slices.Equal(categories, want) {
		t.Errorf("read categories %v, want %v", categories, want)
	}
	charge, _ := json.Marshal(models.ChargeStateFromBle(vd))
	want := `{"charge_state":` + string(charge) + `,"drive_state":{"timestamp":0,"shift_state":"","speed":0,"power":0,"odometer":0}}`
	if string(got) != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func TestVehicleDataJSONErrors(t *testing.T) {
	t.Run("unknown endpoint", func(t *testing.T) {
		calls := 0
		_, retry, err := VehicleDataJSON(context.Background(), []string{"nope"}, func(context.Context, vehicle.StateCategory) (*carserver.VehicleData, error) {
			calls++
			return &carserver.VehicleData{}, nil
		})
		if err == nil || err.Error() != "unrecognized state category 'nope'" || retry || calls != 0 {
			t.Errorf("got (%v, %v), %d reads", retry, err, calls)
		}
	})
	t.Run("read failure", func(t *testing.T) {
		calls := 0
		_, retry, err := VehicleDataJSON(context.Background(), []string{"charge_state", "drive_state"}, func(context.Context, vehicle.StateCategory) (*carserver.VehicleData, error) {
			calls++
			return nil, errors.New("vehicle is sleeping")
		})
		if err == nil || err.Error() != "Failed to get vehicle data: vehicle is sleeping" || !retry || calls != 1 {
			t.Errorf("got (%v, %v), %d reads", retry, err, calls)
		}
	})
}
