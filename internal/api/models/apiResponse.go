package models

import (
	"context"
	"encoding/json"
	"sync"
)

// ApiResponse carries the outcome of a queued command back to the HTTP handler waiting for it.
// The BLE queue writes Result, Error and Response, then calls Finish; the handler reads them
// only after Done is closed.
type ApiResponse struct {
	Result   bool
	Error    string
	Response json.RawMessage
	// Ctx is the context of the HTTP request waiting for the command (nil: nobody waits).
	Ctx context.Context

	done     chan struct{}
	finished sync.Once
}

// NewApiResponse returns the response of a command an HTTP handler waits for; ctx is the
// request context.
func NewApiResponse(ctx context.Context) *ApiResponse {
	return &ApiResponse{Ctx: ctx, done: make(chan struct{})}
}

// Done is closed by Finish. It is nil (never ready) for a response built without NewApiResponse.
func (r *ApiResponse) Done() <-chan struct{} {
	return r.done
}

// Finish releases the handler waiting for the command. Calls after the first are ignored.
func (r *ApiResponse) Finish() {
	r.finished.Do(func() {
		if r.done != nil {
			close(r.done)
		}
	})
}
