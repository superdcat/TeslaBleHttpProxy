package models

// Capabilities is the payload of GET /api/proxy/1/capabilities.
// The lists are sorted sets, never null.
type Capabilities struct {
	API                  int                `json:"api"`
	Version              string             `json:"version"`
	Flavor               string             `json:"flavor"`
	Commands             []string           `json:"commands"`
	VehicleDataEndpoints []string           `json:"vehicle_data_endpoints"`
	ProxyRoutes          []string           `json:"proxy_routes"`
	Features             CapabilityFeatures `json:"features"`
	KeyRole              string             `json:"key_role"`
}

// CapabilityFeatures are the behaviours a client can rely on.
type CapabilityFeatures struct {
	StrictBodyValidation      bool `json:"strict_body_validation"`
	BodyControllerStateQueued bool `json:"body_controller_state_queued"`
	AuthRequired              bool `json:"auth_required"`
}
