package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/3to1go/edge/internal/config"
	_ "github.com/3to1go/edge/internal/schedule"
)

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	resp := a.runner.StatusSnapshot()
	resp["scheduler"] = a.scheduler.Snapshot()
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleRunNow(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	result := a.scheduler.RequestRunNow()
	writeJSON(w, http.StatusOK, map[string]string{"status": result})
}

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	payload := config.SettingsToPayload(a.runner.CurrentSettings())
	writeJSON(w, http.StatusOK, map[string]any{"settings": payload})
}

func (a *App) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var payload config.SettingsPayload
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	newSettings, err := config.BuildSettings(&payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.runner.UpdateSettings(newSettings); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := a.scheduler.ReloadSettings(newSettings.CronSchedule); err != nil {
		a.logger.Warn("scheduler_reload_failed", "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"settings": config.SettingsToPayload(newSettings),
	})
}

func (a *App) handleGetNtfy(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, a.runner.NtfySnapshot(a.runner.CurrentSettings()))
}

func (a *App) handleSaveNtfy(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		NtfyURL             string `json:"ntfy_url"`
		NtfyTopic           string `json:"ntfy_topic"`
		NtfyMessageTemplate string `json:"ntfy_message_template"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	existing := config.SettingsToPayload(a.runner.CurrentSettings())
	existing.NtfyURL = body.NtfyURL
	existing.NtfyTopic = body.NtfyTopic
	existing.NtfyMessageTemplate = body.NtfyMessageTemplate
	newSettings, err := config.BuildSettings(&existing)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.runner.UpdateSettings(newSettings); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.runner.NtfySnapshot(newSettings))
}

func (a *App) handleTestNtfy(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	var body struct {
		NtfyURL             string `json:"ntfy_url"`
		NtfyTopic           string `json:"ntfy_topic"`
		NtfyMessageTemplate string `json:"ntfy_message_template"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.runner.TestNtfy(body.NtfyURL, body.NtfyTopic, body.NtfyMessageTemplate); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleGetCertificates(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, a.runner.CertSnapshot())
}

func (a *App) handleUploadCertificate(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	r.ParseMultipartForm(1 << 20)
	file, header, err := r.FormFile("certificate_file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "certificate_file is required")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	info, err := a.runner.SaveCertFile(header.Filename, content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "file": info})
}

func (a *App) handleDeleteCertificate(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	filename := r.PathValue("filename")
	if err := a.runner.DeleteCertFile(filename); err != nil {
		if strings.HasSuffix(err.Error(), ": not found") {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleGetHooks(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	s := a.runner.CurrentSettings()
	writeJSON(w, http.StatusOK, a.runner.HookSnapshot(s.HookPreCommand, s.HookPostCommand))
}

func (a *App) handleSaveHooks(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		HookPreCommand  string `json:"hook_pre_command"`
		HookPostCommand string `json:"hook_post_command"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	existing := config.SettingsToPayload(a.runner.CurrentSettings())
	existing.HookPreCommand = body.HookPreCommand
	existing.HookPostCommand = body.HookPostCommand
	newSettings, err := config.BuildSettings(&existing)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.runner.UpdateSettings(newSettings); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.runner.HookSnapshot(newSettings.HookPreCommand, newSettings.HookPostCommand))
}

func (a *App) handleUploadHookFile(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	r.ParseMultipartForm(1 << 20)
	file, header, err := r.FormFile("hook_file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "hook_file is required")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	info, err := a.runner.SaveHookFile(header.Filename, content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "file": info})
}

func (a *App) handleViewHookFile(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	filename := r.PathValue("filename")
	name, content, err := a.runner.ReadHookFile(filename)
	if err != nil {
		if strings.HasSuffix(err.Error(), ": not found") {
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
	if err := a.runner.DeleteHookFile(filename); err != nil {
		if strings.HasSuffix(err.Error(), ": not found") {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleGetEncryptionKey(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"fingerprint": a.runner.EncryptionKeyFingerprint(),
		"key_base64":  a.runner.EncryptionKeyBase64(),
	})
}
