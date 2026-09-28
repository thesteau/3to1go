package api

import "net/http"

func (a *App) handleCancelOperation(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	status := "idle"
	if a.runner.CancelOperation() {
		status = "cancelling"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (a *App) handleBrowseFiles(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	entries, err := a.runner.BrowseFiles(r.URL.Query().Get("relative_path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (a *App) handleFolderSize(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	result, err := a.runner.FolderSize(r.Context(), r.URL.Query().Get("relative_path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handlePathAction(w http.ResponseWriter, r *http.Request, action func(string) error) {
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
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := action(body.RelativePath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleClearStaged(w http.ResponseWriter, r *http.Request) {
	handlePathAction(w, r, a.runner.ClearStagedBackup)
}

func (a *App) handleExcludePath(w http.ResponseWriter, r *http.Request) {
	handlePathAction(w, r, a.runner.ExcludePath)
}
