package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Connection hold, in seconds: how long the BLE connection opened for a command stays open.
const (
	DefaultConnectionTimeout = 29 // seconds, connection hold of 2.3.0
	MinConnectionTimeout     = 10
	MaxConnectionTimeout     = 120
)

// btAdapterPattern accepts hci0 to hci15 (the SDK refuses anything above 15), lower case, no
// leading zero.
var btAdapterPattern = regexp.MustCompile(`^hci([0-9]|1[0-5])$`)

// ParseBTAdapter validates the btAdapter variable. Blanks are trimmed; an empty value means the
// default adapter.
func ParseBTAdapter(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !btAdapterPattern.MatchString(value) {
		return "", fmt.Errorf("invalid btAdapter %q: expected hci followed by the adapter number, hci0 to hci15 (for example hci1); leave btAdapter empty to use the default adapter", value)
	}
	return value, nil
}

// ParseConnectionTimeout validates the connectionTimeout variable (whole seconds, 10 to 120).
// An empty value gives the default without error; an invalid one gives the default and an error.
func ParseConnectionTimeout(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return DefaultConnectionTimeout, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < MinConnectionTimeout || seconds > MaxConnectionTimeout {
		return DefaultConnectionTimeout, fmt.Errorf("invalid connectionTimeout %q: expected a whole number of seconds from %d to %d", value, MinConnectionTimeout, MaxConnectionTimeout)
	}
	return seconds, nil
}

// ParseReleaseAdapterWhenIdle validates the releaseAdapterWhenIdle variable. An empty value
// gives false without error; an invalid one gives false and an error.
func ParseReleaseAdapterWhenIdle(raw string) (bool, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false, nil
	}
	release, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid releaseAdapterWhenIdle %q: expected true or false", value)
	}
	return release, nil
}
