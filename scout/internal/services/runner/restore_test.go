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
	for _, test := range []struct {
		name, decision, folder, key string
		cross, fails                bool
	}{
		{name: "default folder", decision: "accept"},
		{name: "renamed folder", decision: "accept", folder: "renamed"},
		{name: "reject", decision: "reject"},
		{name: "cross device requires key", decision: "accept", cross: true, fails: true},
		{name: "cross device wrong key", decision: "accept", cross: true, key: "YWJjZGVmMDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODk=", fails: true},
		{name: "cross device valid key", decision: "accept", cross: true, key: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="},
		{name: "nested folder refused", decision: "accept", folder: "nested/photos", fails: true},
		{name: "scan root refused", decision: "accept", folder: ".", fails: true},
	} {
		decision := test.decision
		t.Run(test.name, func(t *testing.T) {
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
					t.Error("missing Station token")
				}
				switch r.URL.Path {
				case "/backup/recovery/scout-1/instance-1/requests":
					request := protocol.RestoreRequest{ID: "request", ScoutID: "scout-1", ScoutInstanceID: "instance-1", JobName: "photos", Filename: filename, Status: status}
					if test.cross {
						request.SourceScoutID = "source"
						request.SourceInstanceID = "old-instance"
					}
					_ = json.NewEncoder(w).Encode([]protocol.RestoreRequest{request})
				case "/backup/recovery/scout-1/instance-1/requests/request":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					status = body["status"]
					decisions = append(decisions, status)
					writeTestJSON(t, w, map[string]any{"status": status})
				case "/backup/recovery/scout-1/instance-1/requests/request/archive":
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
			result, err := runner.DecideRestoreRequest(context.Background(), "request", decision, test.folder, test.key)
			folder := test.folder
			if folder == "" {
				folder = "photos"
			}
			if test.fails {
				if err == nil {
					t.Fatal("expected restore failure")
				}
				if _, statErr := os.Stat(filepath.Join(settings.ScanRoot, folder, "file.txt")); !os.IsNotExist(statErr) {
					t.Fatal("failed restore wrote files")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(settings.ScanRoot, folder, "file.txt")
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

func TestExpiredRestoreRequestCanBeRejectedButNotAccepted(t *testing.T) {
	for _, test := range []struct {
		name     string
		listed   bool // Station still lists it, but it expires before the decision is saved.
		decision string
		wantErr  bool
	}{
		{"gone, accept", false, "accept", true},
		{"gone, reject", false, "reject", false},
		{"expires while deciding, accept", true, "accept", true},
		{"expires while deciding, reject", true, "reject", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := testRunnerSettings(t)
			if err := os.MkdirAll(settings.ScanRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			station := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/backup/recovery/scout-1/instance-1/requests":
					requests := []protocol.RestoreRequest{}
					if test.listed {
						requests = append(requests, protocol.RestoreRequest{ID: "request", JobName: "photos", Filename: "photos__2026-09-01T00-00-00Z__abcdef12.tar.zst", Status: "pending"})
					}
					_ = json.NewEncoder(w).Encode(requests)
				case "/backup/recovery/scout-1/instance-1/requests/request":
					w.WriteHeader(http.StatusConflict)
					writeTestJSON(t, w, map[string]any{"detail": "request is no longer available"})
				default:
					t.Errorf("expired request reached %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer station.Close()
			settings.StationURL = station.URL
			runner := testRunner(t, settings, testUploadClient(station.URL))
			_, err := runner.DecideRestoreRequest(context.Background(), "request", test.decision, "photos")
			if test.wantErr {
				re, ok := err.(*recovery.RecoveryError)
				if !ok || re.StatusCode != 409 {
					t.Fatalf("err = %v, want expired 409", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(settings.ScanRoot, "photos")); !os.IsNotExist(err) {
				t.Fatal("expired request changed the destination")
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
