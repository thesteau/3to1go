package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/3to1go/shared/httpx"
	"github.com/3to1go/shared/protocol"
)

type restoreRequestStore interface {
	CreateRestoreRequest(context.Context, protocol.RestoreRequest) error
	ListRestoreRequests(context.Context, string, string) ([]protocol.RestoreRequest, error)
	DecideRestoreRequest(context.Context, string, string, string, string) (bool, error)
}

func validRestoreFilename(name string) bool {
	return name != "" && !strings.ContainsAny(name, `/\\`) && strings.HasSuffix(name, ".tar.zst")
}

func (a *App) handleRequestRestore(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	scoutID, instanceID, jobName, filename := r.PathValue("scout_id"), r.PathValue("scout_instance_id"), r.PathValue("job_name"), r.PathValue("filename")
	namespace, err := validatedNamespace(scoutID, instanceID, jobName)
	if err != nil || !validRestoreFilename(filename) {
		httpx.WriteError(w, 400, "invalid snapshot")
		return
	}
	registration, err := a.snapIndex.GetScoutRegistration(r.Context(), scoutID, instanceID)
	if err != nil {
		httpx.WriteError(w, 500, "unable to inspect Scout registration")
		return
	}
	if registration == nil || registration.EncryptionKeyFingerprint == nil || *registration.EncryptionKeyFingerprint == "" {
		httpx.WriteError(w, 409, "Scout must have an encryption key configured")
		return
	}
	file, err := os.OpenInRoot(a.Settings().BackupRoot, snapshotPath(namespace, filename))
	if err != nil {
		httpx.WriteError(w, 404, "snapshot not found")
		return
	}
	info, err := file.Stat()
	_ = file.Close()
	if err != nil || !info.Mode().IsRegular() {
		httpx.WriteError(w, 404, "snapshot not found")
		return
	}
	store := a.snapIndex
	request := protocol.RestoreRequest{ID: rand.Text(), ScoutID: scoutID, ScoutInstanceID: instanceID, JobName: jobName, Filename: filename, Status: "pending", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := store.CreateRestoreRequest(r.Context(), request); err != nil {
		httpx.WriteError(w, 500, "unable to save restore request")
		return
	}
	// The notification is only a hint. Scout verifies the saved request over its
	// authenticated Station connection and never accepts archive URLs from callers.
	notified := false
	if registration.AdvertisedURL != nil {
		endpoint, parseErr := url.Parse(*registration.AdvertisedURL)
		if parseErr == nil && endpoint.Host != "" && (endpoint.Scheme == "http" || endpoint.Scheme == "https") && endpoint.User == nil {
			endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/backup/restore-notifications"
			endpoint.RawQuery, endpoint.Fragment = "", ""
			body, _ := json.Marshal(map[string]string{"id": request.ID})
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			notification, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
			notification.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			if response, err := client.Do(notification); err == nil {
				notified = response.StatusCode == http.StatusOK
				_ = response.Body.Close()
			}
		}
	}
	httpx.WriteJSON(w, 201, map[string]any{"request": request, "notified": notified})
}

func (a *App) authorizeRestoreRequests(w http.ResponseWriter, r *http.Request) bool {
	cred, err := a.authorizeBearer(r)
	if err != nil {
		httpx.WriteError(w, 401, "unauthorized")
		return false
	}
	scoutID, instanceID := r.PathValue("scout_id"), r.PathValue("scout_instance_id")
	if _, err := validatedNamespace(scoutID, instanceID, "restore"); err != nil {
		httpx.WriteError(w, 400, "invalid instance")
		return false
	}
	if status, detail := a.authorizeCredentialForInstance(r, cred, scoutID, instanceID, false); status != 0 {
		httpx.WriteError(w, status, detail)
		return false
	}
	return true
}

func (a *App) handleListRestoreRequests(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeRestoreRequests(w, r) {
		return
	}
	store := a.snapIndex
	requests, err := store.ListRestoreRequests(r.Context(), r.PathValue("scout_id"), r.PathValue("scout_instance_id"))
	if err != nil {
		httpx.WriteError(w, 500, "unable to load restore requests")
		return
	}
	httpx.WriteJSON(w, 200, requests)
}

func (a *App) handleDecideRestoreRequest(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeRestoreRequests(w, r) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if httpx.ReadJSON(r, &body) != nil || (body.Status != "accepted" && body.Status != "rejected" && body.Status != "completed") {
		httpx.WriteError(w, 400, "invalid decision")
		return
	}
	store := a.snapIndex
	changed, err := store.DecideRestoreRequest(r.Context(), r.PathValue("scout_id"), r.PathValue("scout_instance_id"), r.PathValue("request_id"), body.Status)
	if err != nil {
		httpx.WriteError(w, 500, "unable to save decision")
		return
	}
	if !changed {
		httpx.WriteError(w, 409, "request is no longer available")
		return
	}
	httpx.WriteJSON(w, 200, map[string]string{"status": body.Status})
}

func (a *App) handleDownloadExactSnapshot(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeRestoreRequests(w, r) {
		return
	}
	namespace, err := validatedNamespace(r.PathValue("scout_id"), r.PathValue("scout_instance_id"), r.PathValue("job_name"))
	filename := r.PathValue("filename")
	if err != nil || !validRestoreFilename(filename) {
		httpx.WriteError(w, 400, "invalid snapshot")
		return
	}
	a.serveSnapshot(w, r, namespace, filename, true)
}
