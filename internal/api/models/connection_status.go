package models

// ConnectionStatus is the response of the connection_status route: what the BLE scan sees of the
// vehicle. Address and RSSI are null together when the beacon was not seen.
type ConnectionStatus struct {
	LocalName   string  `json:"local_name"`
	Connectable bool    `json:"connectable"`
	Address     *string `json:"address"` // null: beacon not seen
	RSSI        *int16  `json:"rssi"`    // null: beacon not seen (dBm)
	Operated    bool    `json:"operated"`
}
