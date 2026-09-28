package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Cancels during streaming, after discovery and archive setup have completed.
type cancelAfterChecks struct {
	context.Context
	remaining int
	mu        sync.Mutex
}

func (c *cancelAfterChecks) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestArchiveCancellationDuringStreaming(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "large")
	if err := os.WriteFile(source, make([]byte, 4*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterChecks{Context: context.Background(), remaining: 5}
	err := CreateArchiveContext(ctx, filepath.Join(root, "archive.tar.zst"), []*DiscoveredFile{{SourcePath: source, ArchivePath: "large"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestScanCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildFileListContext(ctx, &JobDefinition{RootPath: t.TempDir()}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := DiscoverJobsContext(ctx, t.TempDir(), 2, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
