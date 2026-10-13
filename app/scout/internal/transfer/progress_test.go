package transfer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestObserveStreamsKnownAndUnknownLengths(t *testing.T) {
	for _, total := range []int64{4096, -1} {
		var done, reportedTotal int64
		ctx := WithReporter(context.Background(), func(n, size int64) { done, reportedTotal = n, size })
		content := bytes.Repeat([]byte("x"), 4096)
		reader := Observe(ctx, bytes.NewBuffer(content), total)
		first := make([]byte, 2048)
		if _, err := io.ReadFull(reader, first); err != nil {
			t.Fatal(err)
		}
		if total > 0 && (done != 2048 || reportedTotal != total) {
			t.Fatalf("partial progress: %d/%d", done, reportedTotal)
		}
		rest, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(append(first, rest...), content) {
			t.Fatalf("stream changed: %v", err)
		}
		if done != int64(len(content)) || reportedTotal != max(0, total) {
			t.Fatalf("final progress: %d/%d", done, reportedTotal)
		}
	}
}

func TestObserveStopsReadingAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := bytes.NewBufferString("still unread")
	reader := Observe(ctx, source, int64(source.Len()))
	cancel()
	if _, err := io.ReadAll(reader); !errors.Is(err, context.Canceled) || source.Len() != len("still unread") {
		t.Fatalf("read after cancellation: %v", err)
	}
}
