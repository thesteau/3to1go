package api

import (
	"net/http"

	"github.com/3to1go/edge/internal/services"
)

func (a *App) handleListDirectories(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, a.runner.DirectoriesSnapshot())
}

func (a *App) handleSaveJob(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		RelativePath string                 `json:"relative_path"`
		Config       map[string]interface{} `json:"config"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Config == nil {
		body.Config = map[string]interface{}{}
	}
	entry, err := a.runner.SaveJob(body.RelativePath, body.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "directory": entry})
}

func (a *App) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		RelativePath string `json:"relative_path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.runner.DeleteJob(body.RelativePath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleForceSend(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		JobName string `json:"job_name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := a.runner.ForceSendJob(r.Context(), body.JobName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleRecoveryPreview(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		RelativePath string `json:"relative_path"`
		Fingerprint  string `json:"fingerprint"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := a.runner.PreviewRecovery(r.Context(), body.RelativePath, body.Fingerprint)
	if err != nil {
		re, ok := err.(*services.RecoveryError)
		if ok {
			writeError(w, re.StatusCode, re.Message)
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleRecoveryRestore(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		RelativePath string `json:"relative_path"`
		Fingerprint  string `json:"fingerprint"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := a.runner.RecoverJob(r.Context(), body.RelativePath, body.Fingerprint)
	if err != nil {
		re, ok := err.(*services.RecoveryError)
		if ok {
			writeError(w, re.StatusCode, re.Message)
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}
