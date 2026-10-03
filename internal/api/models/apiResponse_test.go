package models

import (
	"context"
	"testing"
	"time"
)

func TestApiResponseFinish(t *testing.T) {
	t.Run("closes Done once, further calls are ignored", func(t *testing.T) {
		r := NewApiResponse(context.Background())
		select {
		case <-r.Done():
			t.Fatal("Done is ready before Finish")
		default:
		}
		r.Finish()
		r.Finish() // must not panic
		select {
		case <-r.Done():
		case <-time.After(time.Second):
			t.Fatal("Done not closed after Finish")
		}
	})

	t.Run("keeps the request context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if r := NewApiResponse(ctx); r.Ctx != ctx {
			t.Fatal("Ctx is not the request context")
		}
	})

	t.Run("zero value: Finish is harmless and Done never ready", func(t *testing.T) {
		var r ApiResponse
		r.Finish()
		r.Finish()
		if r.Done() != nil {
			t.Fatal("Done of a zero value must be nil")
		}
	})
}
