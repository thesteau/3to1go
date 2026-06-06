package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/ingest"
	"github.com/3to1go/central/internal/services/overview"
	"github.com/3to1go/central/internal/storage"
)

func (a *App) handleOverview(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	s := a.Settings()
	data, err := overview.BuildOverview(r.Context(), s, a.backend, a.snapIndex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build overview")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (a *App) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body config.SettingsPayload
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	newSettings, err := config.BuildSettings(&body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized := config.SettingsToPayload(newSettings)
	if err := a.settingsStore.Save(r.Context(), &normalized); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	a.ApplySettings(newSettings)
	a.RestartCleanupLoop(newSettings.UploadCleanupIntervalS)

	data, err := overview.BuildOverview(r.Context(), newSettings, a.backend, a.snapIndex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build overview")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"settings": data["settings"],
	})
}

func (a *App) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	edgeID, err := ingest.ValidateNamespaceComponent(r.PathValue("edge_id"), "edge_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	instID, err := ingest.ValidateNamespaceComponent(r.PathValue("edge_instance_id"), "edge_instance_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cleanupMissing := r.URL.Query().Get("cleanup_missing") == "true"

	s := a.Settings()
	instanceDir := filepath.Join(s.BackupRoot, edgeID, instID)

	info, err := os.Stat(instanceDir)
	if err != nil || !info.IsDir() {
		reg, err := a.snapIndex.GetEdgeRegistration(r.Context(), edgeID, instID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to inspect instance registration")
			return
		}
		if reg != nil {
			if cleanupMissing {
				if err := a.snapIndex.DeleteEdgeRegistration(r.Context(), edgeID, instID); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to clean instance registration")
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{
					"status":           "cleaned",
					"edge_id":          edgeID,
					"edge_instance_id": instID,
				})
				return
			}
			writeError(w, http.StatusConflict, map[string]any{
				"message":           "instance files not found",
				"cleanup_available": true,
			})
			return
		}
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}

	// Collect job namespaces before deleting
	entries, _ := os.ReadDir(instanceDir)
	var namespaces []string
	for _, e := range entries {
		if e.IsDir() {
			namespaces = append(namespaces, edgeID+"/"+instID+"/"+e.Name())
		}
	}

	if err := os.RemoveAll(instanceDir); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete instance files")
		return
	}

	for _, ns := range namespaces {
		a.ingest.ReconcileNamespace(r.Context(), ns)
	}

	// Check if anything remains
	if _, err := os.Stat(instanceDir); err == nil {
		writeError(w, http.StatusConflict, map[string]any{
			"message":           "instance still has backup files or index entries",
			"cleanup_available": false,
		})
		return
	}
	hasEntries, _ := a.snapIndex.HasNamespaceEntries(r.Context(), edgeID, instID)
	if hasEntries {
		writeError(w, http.StatusConflict, map[string]any{
			"message":           "instance still has backup files or index entries",
			"cleanup_available": false,
		})
		return
	}
	if err := a.snapIndex.DeleteEdgeRegistration(r.Context(), edgeID, instID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete instance registration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":           "deleted",
		"edge_id":          edgeID,
		"edge_instance_id": instID,
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	s := a.Settings()
	if !a.backend.Healthcheck() {
		writeError(w, http.StatusServiceUnavailable, "storage backend unavailable")
		return
	}

	stagingUsed := storage.DirSize(s.StagingDir)
	_, _, stagingFree, _ := storage.DiskUsage(s.StagingDir)
	backupUsed := storage.DirSize(s.BackupRoot)
	_, _, backupFree, _ := storage.DiskUsage(s.BackupRoot)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                       "ok",
		"staging_dir":                  s.StagingDir,
		"staging_used_bytes":           stagingUsed,
		"staging_free_bytes":           stagingFree,
		"backup_root":                  s.BackupRoot,
		"backup_used_bytes":            backupUsed,
		"backup_free_bytes":            backupFree,
		"max_upload_size_bytes":        s.MaxUploadSizeBytes(),
		"recommended_chunk_size_bytes": s.UploadChunkSizeBytes(),
	})
}

func (a *App) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	s := a.Settings()
	if !a.backend.Healthcheck() {
		writeError(w, http.StatusServiceUnavailable, "storage backend unavailable")
		return
	}
	os.MkdirAll(s.StagingDir, 0o755)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
