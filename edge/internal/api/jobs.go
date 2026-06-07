package api

import (
	"net/http"

	"github.com/3to1go/edge/internal/services/recovery"
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
		RelativePath string         `json:"relative_path" validate:"required"`
		Config       map[string]any `json:"config"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateStruct(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid job request")
		return
	}
	if body.Config == nil {
		body.Config = map[string]any{}
	}
	entry, err := a.runner.SaveJob(body.RelativePath, body.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "directory": entry})
}

func (a *App) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		RelativePath string `json:"relative_path" validate:"required"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateStruct(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid job request")
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
		RelativePath string `json:"relative_path" validate:"required"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateStruct(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid job request")
		return
	}
	result, err := a.runner.StartForceSendAsync(body.RelativePath)
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
		RelativePath string `json:"relative_path" validate:"required"`
		Fingerprint  string `json:"fingerprint"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateStruct(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid recovery request")
		return
	}
	result, err := a.runner.PreviewRecovery(r.Context(), body.RelativePath, body.Fingerprint)
	if err != nil {
		re, ok := err.(*recovery.RecoveryError)
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
		RelativePath string `json:"relative_path" validate:"required"`
		Fingerprint  string `json:"fingerprint"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateStruct(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid recovery request")
		return
	}
	result, err := a.runner.RecoverJob(r.Context(), body.RelativePath, body.Fingerprint)
	if err != nil {
		re, ok := err.(*recovery.RecoveryError)
		if ok {
			writeError(w, re.StatusCode, re.Message)
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}
