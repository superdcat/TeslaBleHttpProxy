// Commands whose HTTP client stopped waiting are not sent to the vehicle.
//
// Adapted from Lenart12/TeslaBleHttpProxy (commits 64390c6, 40cb54b, 82c4113, 980553e),
// Copyright Lenart12 and contributors, Apache License 2.0. Rewritten in the superdcat fork
// for the queue of wimaha 2.3.0 (no copy of Lenart12's control.go).

package control

import (
	"github.com/wimaha/TeslaBleHttpProxy/internal/logging"
	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// Stages at which an abandoned command stops (field "Stage" of the log line).
// Field "Attempts" counts the sendings already handed to the SDK: 0 means nothing was sent.
const (
	stageQueue   = "queue"   // taken from the queue
	stageRequeue = "requeue" // handed back for a new connection, before reconnecting
	stageConnect = "connect" // while connecting to or waking up the vehicle
	stageSend    = "send"    // connection ready, before sending
	stageRetry   = "retry"   // between two sendings
)

// Boundaries of the queue with the vehicle, replaced in tests (no adapter, no vehicle).
var (
	tryConnectToVehicle = (*BleControl).TryConnectToVehicle
	sendCommand         = (*commands.Command).Send
)

// logAbandoned records that command stops because its client stopped waiting.
func logAbandoned(command *commands.Command, stage string, lastErr error) {
	args := []interface{}{"Command", command.Command, "Stage", stage, "Attempts", command.SendAttempts, "Reason", command.Abandoned()}
	if lastErr != nil {
		args = append(args, "LastError", lastErr)
	}
	logging.Info("Command abandoned, client stopped waiting", args...)
}

// skipAbandonedCommand finishes command as failed, without contacting the vehicle, when its
// client stopped waiting, and reports whether it did. The caller must not finish it again.
func skipAbandonedCommand(command *commands.Command, stage string) bool {
	err := command.Abandoned()
	if err == nil {
		return false
	}
	logAbandoned(command, stage, nil)
	command.Response.Error = err.Error()
	command.Response.Result = false
	command.Response.Finish()
	return true
}
