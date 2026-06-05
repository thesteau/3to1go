package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/relay/central/internal/ingest"
)

func validatedNamespace(edgeID, instID, jobName string) (string, error) {
	edgeID, err := ingest.ValidateNamespaceComponent(edgeID, "edge_id")
	if err != nil {
		return "", err
	}
	instID, err = ingest.ValidateNamespaceComponent(instID, "edge_instance_id")
	if err != nil {
		return "", err
	}
	jobName, err = ingest.ValidateNamespaceComponent(jobName, "job_name")
	if err != nil {
		return "", err
	}
	return edgeID + "/" + instID + "/" + jobName, nil
}

func validatedLegacyNamespace(edgeID, jobName string) (string, error) {
	edgeID, err := ingest.ValidateNamespaceComponent(edgeID, "edge_id")
	if err != nil {
		return "", err
	}
	jobName, err = ingest.ValidateNamespaceComponent(jobName, "job_name")
	if err != nil {
		return "", err
	}
	return edgeID + "/" + jobName, nil
}

func snapshotPath(namespace, filename string) string {
	return filepath.Join(filepath.FromSlash(namespace), filename)
}

func (a *App) serveSnapshot(w http.ResponseWriter, r *http.Request, namespace, filename string, includeSnapshotHeader bool) {
	file, err := os.OpenInRoot(a.Settings().BackupRoot, snapshotPath(namespace, filename))
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "snapshot not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	if includeSnapshotHeader {
		w.Header().Set("X-Relay-Snapshot-Filename", filename)
	}
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

func (a *App) removeSnapshot(namespace, filename string) error {
	root, err := os.OpenRoot(a.Settings().BackupRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(snapshotPath(namespace, filename))
}

func (a *App) handleDownloadSnapshotForInstance(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	edgeID := r.PathValue("edge_id")
	instID := r.PathValue("edge_instance_id")
	jobName := r.PathValue("job_name")
	filename := r.PathValue("filename")

	namespace, err := validatedNamespace(edgeID, instID, jobName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.serveSnapshot(w, r, namespace, filename, false)
}

func (a *App) handleDownloadSnapshot(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	edgeID := r.PathValue("edge_id")
	jobName := r.PathValue("job_name")
	filename := r.PathValue("filename")

	namespace, err := validatedLegacyNamespace(edgeID, jobName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.serveSnapshot(w, r, namespace, filename, false)
}

func (a *App) handleDeleteSnapshotForInstance(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	edgeID := r.PathValue("edge_id")
	instID := r.PathValue("edge_instance_id")
	jobName := r.PathValue("job_name")
	filename := r.PathValue("filename")

	namespace, err := validatedNamespace(edgeID, instID, jobName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.removeSnapshot(namespace, filename); err != nil {
		if !os.IsNotExist(err) {
			writeError(w, http.StatusBadRequest, "invalid path")
			return
		}
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	a.ingest.ReconcileNamespace(r.Context(), namespace)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "filename": filename})
}

func (a *App) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	edgeID := r.PathValue("edge_id")
	jobName := r.PathValue("job_name")
	filename := r.PathValue("filename")

	namespace, err := validatedLegacyNamespace(edgeID, jobName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.removeSnapshot(namespace, filename); err != nil {
		if !os.IsNotExist(err) {
			writeError(w, http.StatusBadRequest, "invalid path")
			return
		}
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	a.ingest.ReconcileNamespace(r.Context(), namespace)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "filename": filename})
}

func (a *App) handleDownloadLatest(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authorizeBearer(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	edgeID := r.PathValue("edge_id")
	instID := r.PathValue("edge_instance_id")
	jobName := r.PathValue("job_name")
	namespace, err := validatedNamespace(edgeID, instID, jobName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	files, err := a.backend.List(namespace)
	if err != nil || len(files) == 0 {
		writeError(w, http.StatusNotFound, "no snapshots found")
		return
	}

	best := files[0]
	for _, f := range files[1:] {
		if f.Mtime > best.Mtime || (f.Mtime == best.Mtime && f.Filename > best.Filename) {
			best = f
		}
	}

	a.serveSnapshot(w, r, namespace, best.Filename, true)
}

func (a *App) handleDownloadByFingerprint(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authorizeBearer(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	edgeID := r.PathValue("edge_id")
	instID := r.PathValue("edge_instance_id")
	jobName := r.PathValue("job_name")
	fp := strings.TrimSpace(r.URL.Query().Get("fp"))
	if fp == "" {
		writeError(w, http.StatusBadRequest, "fp parameter is required")
		return
	}
	namespace, err := validatedNamespace(edgeID, instID, jobName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	files, err := a.backend.List(namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list snapshots")
		return
	}

	type fileMatch struct {
		mtime    float64
		filename string
	}
	var matchSlice []fileMatch
	for _, f := range files {
		if strings.Contains(f.Filename, fp) {
			matchSlice = append(matchSlice, fileMatch{mtime: f.Mtime, filename: f.Filename})
		}
	}

	if len(matchSlice) == 0 {
		writeError(w, http.StatusNotFound, "no snapshot found with that fingerprint")
		return
	}
	best := matchSlice[0]
	for _, m := range matchSlice[1:] {
		if m.mtime > best.mtime {
			best = m
		}
	}

	a.serveSnapshot(w, r, namespace, best.filename, true)
}
