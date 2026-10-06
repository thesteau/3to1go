package api

import (
	"context"
	"net/http"

	"github.com/3to1go/scout/internal/services/recovery"
	"github.com/3to1go/shared/httpx"
	"github.com/3to1go/shared/protocol"
)

type restoreRunner interface {
	RestoreRequests(context.Context) ([]protocol.RestoreRequest, error)
	DecideRestoreRequest(context.Context, string, string, string, ...string) (any, error)
}

func (a *App) handleRestoreRequests(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	runner := a.runner
	requests, err := runner.RestoreRequests(r.Context())
	if err != nil {
		httpx.WriteError(w, 502, "Station restore requests unavailable")
		return
	}
	httpx.WriteJSON(w, 200, requests)
}

func (a *App) handleRestoreDecision(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		EncryptionKey string `json:"encryption_key"`
		ID            string `json:"id"`
		Decision      string `json:"decision"`
		RelativePath  string `json:"relative_path"`
	}
	if httpx.ReadJSON(r, &body) != nil || body.ID == "" {
		httpx.WriteError(w, 400, "invalid decision")
		return
	}
	runner := a.runner
	result, err := runner.DecideRestoreRequest(r.Context(), body.ID, body.Decision, body.RelativePath, body.EncryptionKey)
	if err != nil {
		if re, ok := err.(*recovery.RecoveryError); ok {
			httpx.WriteError(w, re.StatusCode, re.Message)
		} else {
			httpx.WriteError(w, 400, err.Error())
		}
		return
	}
	httpx.WriteJSON(w, 200, result)
}

func (a *App) handleRestoreNotification(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		ID string `json:"id"`
	}
	if httpx.ReadJSON(r, &body) != nil || body.ID == "" || len(body.ID) > 64 {
		httpx.WriteError(w, 400, "invalid notification")
		return
	}
	runner := a.runner
	// Only requests retrieved with this Scout's credential can appear in its UI.
	requests, err := runner.RestoreRequests(r.Context())
	if err != nil {
		httpx.WriteError(w, 502, "cannot verify Station notification")
		return
	}
	for _, request := range requests {
		if request.ID == body.ID {
			httpx.WriteJSON(w, 200, map[string]string{"status": "notified"})
			return
		}
	}
	httpx.WriteError(w, 404, "restore request not found")
}
