package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/services/certificates"
)

// --- Certificates ---

func (a *App) handleGetCertificates(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, a.certs.Snapshot())
}

func (a *App) handleUploadCertificate(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	r.ParseMultipartForm(10 << 20)
	file, header, err := r.FormFile("certificate_file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "certificate_file is required")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	saved, err := a.certs.SaveUploadedFile(header.Filename, content)
	if err != nil {
		if isRuntimeError(err) {
			writeError(w, http.StatusBadGateway, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "file": saved})
}

func (a *App) handleDeleteCertificate(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	filename := r.PathValue("filename")
	if err := a.certs.DeleteFile(filename); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Ntfy ---

func (a *App) handleGetNtfy(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, a.ntfy.Snapshot(a.Settings()))
}

func (a *App) handleSaveNtfy(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		NtfyURL             string `json:"ntfy_url"`
		NtfyTopic           string `json:"ntfy_topic"`
		NtfyMessageTemplate string `json:"ntfy_message_template"`
		NtfyMatchEdgeID     string `json:"ntfy_match_edge_id"`
		NtfyMatchEdgeInstID string `json:"ntfy_match_edge_instance_id"`
		NtfyMatchSource     string `json:"ntfy_match_source"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s := a.Settings()
	payload := config.SettingsToPayload(s)
	payload.NtfyURL = body.NtfyURL
	payload.NtfyTopic = body.NtfyTopic
	payload.NtfyMessageTemplate = body.NtfyMessageTemplate
	payload.NtfyMatchEdgeID = body.NtfyMatchEdgeID
	payload.NtfyMatchEdgeInstID = body.NtfyMatchEdgeInstID
	payload.NtfyMatchSource = body.NtfyMatchSource

	if err := a.settingsStore.Save(r.Context(), &payload); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	newSettings, err := config.BuildSettings(&payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.ApplySettings(newSettings)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"ntfy": map[string]string{
			"ntfy_url":                    body.NtfyURL,
			"ntfy_topic":                  body.NtfyTopic,
			"ntfy_message_template":       body.NtfyMessageTemplate,
			"ntfy_match_edge_id":          body.NtfyMatchEdgeID,
			"ntfy_match_edge_instance_id": body.NtfyMatchEdgeInstID,
			"ntfy_match_source":           body.NtfyMatchSource,
		},
	})
}

func (a *App) handleTestNtfy(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.ntfy.PublishTest(body); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Hooks ---

func (a *App) handleGetHooks(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	s := a.Settings()
	writeJSON(w, http.StatusOK, a.hooks.Snapshot(s.HookPreCommand, s.HookPostCommand))
}

func (a *App) handleSaveHooks(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		PreCommand  string `json:"pre_command"`
		PostCommand string `json:"post_command"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s := a.Settings()
	payload := config.SettingsToPayload(s)
	payload.HookPreCommand = body.PreCommand
	payload.HookPostCommand = body.PostCommand

	if err := a.settingsStore.Save(r.Context(), &payload); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	newSettings, err := config.BuildSettings(&payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.ApplySettings(newSettings)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"hooks":  map[string]string{"pre_command": body.PreCommand, "post_command": body.PostCommand},
	})
}

func (a *App) handleUploadHookFile(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	r.ParseMultipartForm(10 << 20)
	file, header, err := r.FormFile("hook_file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "hook_file is required")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	saved, err := a.hooks.SaveUploadedFile(header.Filename, content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "file": saved})
}

func (a *App) handleViewHookFile(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	filename := r.PathValue("filename")
	name, content, err := a.hooks.ReadTextFile(filename)
	if err != nil {
		if isNotFoundError(err) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"filename": name, "content": content})
}

func (a *App) handleDeleteHookFile(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	filename := r.PathValue("filename")
	if err := a.hooks.DeleteFile(filename); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func isRuntimeError(err error) bool {
	_, ok := errors.AsType[*certificates.ExecError](err)
	return ok
}

func isNotFoundError(err error) bool {
	return err != nil && (len(err.Error()) > 9 && err.Error()[len(err.Error())-9:] == "not found")
}
