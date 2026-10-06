package upload

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryWaitIsCancellable(t *testing.T) {
	client := NewTestUploadClient("http://unused")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		_, err := client.retryPhaseContext(ctx, "initiate", func() (map[string]any, error) {
			close(started)
			delay := 300
			return nil, &UploadFailure{Retryable: true, RetryAfterSeconds: &delay}
		})
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry delay did not cancel")
	}
}
