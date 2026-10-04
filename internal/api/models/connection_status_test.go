package models

import (
	"encoding/json"
	"testing"
)

func TestConnectionStatusJSON(t *testing.T) {
	address, rssi := "aa:bb:cc:dd:ee:ff", int16(-67)
	tests := []struct {
		name   string
		status ConnectionStatus
		want   string
	}{
		{"seen", ConnectionStatus{LocalName: "S0123456789abcdefC", Connectable: true, Address: &address, RSSI: &rssi},
			`{"local_name":"S0123456789abcdefC","connectable":true,"address":"aa:bb:cc:dd:ee:ff","rssi":-67,"operated":false}`},
		{"seen, operated, not connectable", ConnectionStatus{LocalName: "S0123456789abcdefC", Address: &address, RSSI: &rssi, Operated: true},
			`{"local_name":"S0123456789abcdefC","connectable":false,"address":"aa:bb:cc:dd:ee:ff","rssi":-67,"operated":true}`},
		{"absent", ConnectionStatus{LocalName: "S0123456789abcdefC"},
			`{"local_name":"S0123456789abcdefC","connectable":false,"address":null,"rssi":null,"operated":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.status)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
			var keys map[string]json.RawMessage
			if err := json.Unmarshal(got, &keys); err != nil || len(keys) != 5 {
				t.Errorf("%d keys (%v), want exactly 5", len(keys), err)
			}
		})
	}
}
