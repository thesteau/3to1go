package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/encryption"
	"github.com/3to1go/scout/internal/services/recovery"
	"github.com/3to1go/shared/protocol"
)

func TestRestoreDestinationAllowsDeletedFolder(t *testing.T) {
	root := t.TempDir()
	got, err := restoreDestination(root, "deleted/subfolder")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "deleted", "subfolder") {
		t.Fatalf("destination %q", got)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatal("validation must not create destination")
	}
}

func TestRestoreRequestDecision(t *testing.T) {
	for _, decision := range []string{"accept", "reject"} {
		t.Run(decision, func(t *testing.T) {
			settings := testRunnerSettings(t)
			if err := os.MkdirAll(settings.ScanRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			key := []byte("0123456789abcdef0123456789abcdef")
			source := filepath.Join(t.TempDir(), "file.txt")
			if err := os.WriteFile(source, []byte("selected older archive"), 0o640); err != nil {
				t.Fatal(err)
			}
			archive, encrypted := filepath.Join(t.TempDir(), "archive.tar.zst"), filepath.Join(t.TempDir(), "encrypted.tar.zst")
			if err := backup.CreateArchive(archive, []*backup.DiscoveredFile{{SourcePath: source, ArchivePath: "file.txt"}}); err != nil {
				t.Fatal(err)
			}
			if err := encryption.EncryptFile(key, archive, encrypted); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(encrypted)
			if err != nil {
				t.Fatal(err)
			}
			filename := "photos__2026-09-01T00-00-00Z__abcdef12.tar.zst"
			status := "pending"
			var decisions []string
			downloads := 0
			station := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer scout-secret" {
					t.Error("missing Scout credential")
				}
				switch r.URL.Path {
				case "/backup/recovery/scout-1/instance-1/requests":
					_ = json.NewEncoder(w).Encode([]protocol.RestoreRequest{{ID: "request", JobName: "photos", Filename: filename, Status: status}})
				case "/backup/recovery/scout-1/instance-1/requests/request":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					status = body["status"]
					decisions = append(decisions, status)
					writeTestJSON(t, w, map[string]any{"status": status})
				case "/backup/recovery/scout-1/instance-1/photos/archive/" + filename:
					downloads++
					if status != "accepted" {
						t.Error("download before acceptance")
					}
					w.Header().Set("X-Relay-Snapshot-Filename", filename)
					_, _ = w.Write(content)
				default:
					t.Errorf("unexpected download or endpoint %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer station.Close()
			settings.StationURL = station.URL
			client := testUploadClient(station.URL)
			runner := testRunner(t, settings, client)
			runner.Recovery = recovery.NewRecoveryService(settings, runner.logger, runner.StateStore, client, key)
			result, err := runner.DecideRestoreRequest(context.Background(), "request", decision, "deleted/photos")
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(settings.ScanRoot, "deleted", "photos", "file.txt")
			if decision == "reject" {
				if downloads != 0 || status != "rejected" {
					t.Fatalf("rejection downloaded %d archives, status %s", downloads, status)
				}
				if _, err := os.Stat(destination); !os.IsNotExist(err) {
					t.Fatal("rejection changed destination")
				}
			} else {
				if downloads != 1 || len(decisions) != 2 || decisions[0] != "accepted" || status != "completed" {
					t.Fatalf("downloads %d, decisions %v", downloads, decisions)
				}
				data, err := os.ReadFile(destination)
				if err != nil || string(data) != "selected older archive" {
					t.Fatalf("restored %q, err %v", data, err)
				}
				if result.(*recovery.RecoveryResult).SnapshotFilename != filename {
					t.Fatalf("wrong archive: %+v", result)
				}
			}
		})
	}
}

func TestRestoreDestinationRejectsTraversalAndRuntime(t *testing.T) {
	root := t.TempDir()
	if err := backup.MarkRuntimeDir(filepath.Join(root, "state")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "../outside", filepath.Join(root, "absolute"), "state/deleted"} {
		if got, err := restoreDestination(root, path); err == nil {
			t.Errorf("accepted %q as %q", path, got)
		}
	}
}

func TestRestoreDestinationRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := restoreDestination(root, "link/deleted"); err == nil {
		t.Fatal("accepted destination through symlink")
	}
}
