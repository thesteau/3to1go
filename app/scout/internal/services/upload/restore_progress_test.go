package upload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/3to1go/scout/internal/transfer"
)

func TestRestoreDownloadReportsKnownAndUnknownLengths(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(strconv.FormatBool(known), func(t *testing.T) {
			content := "encrypted snapshot bytes"
			station := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/backup/recovery/scout-1/instance-1/requests/request/archive" || r.Header.Get("Authorization") != "Bearer scout-secret" {
					t.Errorf("wrong restore request: %s", r.URL.Path)
				}
				w.Header().Set("X-Relay-Snapshot-Filename", "exact.tar.zst")
				if known {
					w.Header().Set("Content-Length", strconv.Itoa(len(content)))
				} else {
					w.(http.Flusher).Flush()
				}
				if _, err := w.Write([]byte(content)); err != nil {
					t.Error(err)
				}
			}))
			defer station.Close()
			var done, total int64
			ctx := transfer.WithReporter(context.Background(), func(n, size int64) { done, total = n, size })
			path := filepath.Join(t.TempDir(), "download")
			filename, err := testUploadClient(station.URL).DownloadRestoreRequest(ctx, "scout-1", "request", path)
			if err != nil || filename != "exact.tar.zst" {
				t.Fatalf("download: %s, %v", filename, err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != content || done != int64(len(content)) {
				t.Fatalf("downloaded content/progress: %q, %d, %v", got, done, err)
			}
			wantTotal := int64(0)
			if known {
				wantTotal = int64(len(content))
			}
			if total != wantTotal {
				t.Fatalf("total %d, want %d", total, wantTotal)
			}
		})
	}
}
