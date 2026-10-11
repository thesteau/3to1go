package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/3to1go/shared/certificates"
	"github.com/3to1go/shared/httpx"
	"github.com/3to1go/station/internal/config"
)

// --- Certificates ---

func (a *App) handleGetCertificates(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.certs.Snapshot())
}

func (a *App) handleUploadCertificate(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "certificate_file is required")
		return
	}
	file, header, err := r.FormFile("certificate_file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "certificate_file is required")
		return
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	saved, err := a.certs.SaveUploadedFile(header.Filename, content)
	if err != nil {
		if isRuntimeError(err) {
			httpx.WriteError(w, http.StatusBadGateway, err.Error())
		} else {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "file": saved})
}

func (a *App) handleDeleteCertificate(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	filename := r.PathValue("filename")
	if err := a.certs.DeleteFile(filename); err != nil {
		httpx.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Hooks ---

func (a *App) handleGetHooks(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	s := a.Settings()
	httpx.WriteJSON(w, http.StatusOK, a.hooks.Snapshot(s.HookPreCommand, s.HookPostCommand))
}

func (a *App) handleSaveHooks(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		PreCommand  string `json:"pre_command"`
		PostCommand string `json:"post_command"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s := a.Settings()
	payload := config.SettingsToPayload(s)
	payload.HookPreCommand = body.PreCommand
	payload.HookPostCommand = body.PostCommand

	if err := a.settingsStore.Save(r.Context(), &payload); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	newSettings, err := config.BuildSettings(&payload)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.ApplySettings(newSettings)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"hooks":  map[string]string{"pre_command": body.PreCommand, "post_command": body.PostCommand},
	})
}

func (a *App) handleUploadHookFile(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "hook_file is required")
		return
	}
	file, header, err := r.FormFile("hook_file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "hook_file is required")
		return
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	saved, err := a.hooks.SaveUploadedFile(header.Filename, content)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "file": saved})
}

func (a *App) handleViewHookFile(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	filename := r.PathValue("filename")
	name, content, err := a.hooks.ReadTextFile(filename)
	if err != nil {
		if isNotFoundError(err) {
			httpx.WriteError(w, http.StatusNotFound, err.Error())
		} else {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"filename": name, "content": content})
}

func (a *App) handleDeleteHookFile(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	filename := r.PathValue("filename")
	if err := a.hooks.DeleteFile(filename); err != nil {
		httpx.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func isRuntimeError(err error) bool {
	_, ok := errors.AsType[*certificates.ExecError](err)
	return ok
}

func isNotFoundError(err error) bool {
	return err != nil && (len(err.Error()) > 9 && err.Error()[len(err.Error())-9:] == "not found")
}
