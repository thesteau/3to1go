package api

import (
	"net/http"
)

func (a *App) handleGetVerifyStatus(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	if a.verify == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "never_run"})
		return
	}
	result := a.verify.Latest()
	if result == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "never_run"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"checked_at":       result.CheckedAt,
		"total_checked":    result.TotalChecked,
		"failure_count":    result.FailureCount,
		"last_failure":     result.LastFailure,
		"last_failure_msg": result.LastFailureMsg,
	})
}

func (a *App) handleRunVerify(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	if a.verify == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "never_run", "total_checked": 0, "failure_count": 0})
		return
	}
	result := a.verify.RunOnce(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"checked_at":       result.CheckedAt,
		"total_checked":    result.TotalChecked,
		"failure_count":    result.FailureCount,
		"last_failure":     result.LastFailure,
		"last_failure_msg": result.LastFailureMsg,
	})
}
