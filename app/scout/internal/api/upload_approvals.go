package api

import (
	"net/http"

	"github.com/3to1go/shared/httpx"
)

func (a *App) handleLargeUploadApprovals(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	approvals, err := a.runner.LargeUploadApprovals(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to load large-upload approvals")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"approvals": approvals})
}

func (a *App) handleRemoveLargeUploadApproval(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	if err := a.runner.RemoveLargeUploadApproval(r.Context(), r.PathValue("approval_id")); err != nil {
		httpx.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
